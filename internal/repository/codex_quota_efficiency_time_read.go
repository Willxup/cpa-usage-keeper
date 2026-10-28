package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// 游标只持有周期/区间位置，不保留已读事件；额外内存与整月请求数量无关。
type codexQuotaEfficiencyUsageCursor struct {
	works           []codexQuotaEfficiencyCycleWork
	workIndex       int
	transitionIndex int
	previousAt      time.Time
}

func streamCodexQuotaEfficiencyUsageRead(ctx context.Context, db *gorm.DB, provider, authIndex string, read usageEventTimeRead, cursor *codexQuotaEfficiencyUsageCursor) error {
	// 同一账号热更新后可能换 provider；保留只读路由和账号/时间索引范围。
	// 一元 + 保留原值类型并绕过 DATETIME 解析；排序限定原列，避免 alias 触发临时排序。
	rows, err := read.query(db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("usage_events INDEXED BY idx_usage_events_auth_index_timestamp_id").
		Select(`+timestamp AS timestamp, COALESCE(failed, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), COALESCE(reasoning_tokens, 0),
			cache_read_tokens, cache_creation_tokens, COALESCE(total_tokens, 0), cost_usd, cost_available`).
		Where("auth_type = ? AND auth_index = ? AND provider = ?", "oauth", authIndex, provider)).
		Order("usage_events.timestamp ASC, usage_events.id ASC").Rows()
	if err != nil {
		return fmt.Errorf("stream codex quota efficiency usage: %w", err)
	}
	// 每段结束或出错即释放当前 reader，下一段始终只打开一个游标。
	defer rows.Close()
	var event codexQuotaEfficiencyUsageEventRow
	var storedTimestamp codexQuotaEfficiencyEventTime
	var storedCost sql.NullFloat64
	var storedAvailable sql.NullBool
	for rows.Next() {
		if err := rows.Scan(
			&storedTimestamp, &event.Failed, &event.InputTokens, &event.OutputTokens, &event.ReasoningTokens,
			&event.CacheReadTokens, &event.CacheCreationTokens, &event.TotalTokens, &storedCost, &storedAvailable,
		); err != nil {
			return fmt.Errorf("scan codex quota efficiency usage: %w", err)
		}
		event.Timestamp = timeutil.NormalizeStorageTime(storedTimestamp.Time)
		// UTC 边界秒的候选范围较宽；真实时间裁剪必须先于费用校验和任何统计。
		if !read.contains(event.Timestamp) {
			continue
		}
		work, transition := cursor.locate(event.Timestamp)
		if work == nil {
			continue
		}
		if !storedCost.Valid || !storedAvailable.Valid || math.IsNaN(storedCost.Float64) || math.IsInf(storedCost.Float64, 0) {
			return fmt.Errorf("codex quota efficiency usage has unbackfilled or invalid stored cost")
		}
		event.CostUSD, event.CostAvailable = storedCost.Float64, storedAvailable.Bool
		if err := addCodexQuotaEfficiencyUsage(&work.record.Usage, event); err != nil {
			return fmt.Errorf("codex quota efficiency cycle: %w", err)
		}
		if transition != nil {
			if err := addCodexQuotaEfficiencyUsage(&transition.Usage, event); err != nil {
				return fmt.Errorf("codex quota efficiency transition: %w", err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate codex quota efficiency usage: %w", err)
	}
	return nil
}

func (c *codexQuotaEfficiencyUsageCursor) locate(at time.Time) (*codexQuotaEfficiencyCycleWork, *repositorydto.CodexQuotaEfficiencyTransition) {
	backwards := at.Before(c.previousAt)
	c.previousAt = at
	if backwards {
		// RFC3339 的可变小数位不保证文本与 instant 同序；倒序时按周期右边界二分恢复。
		// 有效周期已经按后续理论起点切分且互斥，因此排序后的右边界同样递增。
		c.workIndex = sort.Search(len(c.works), func(index int) bool { return at.Before(c.works[index].queryEnd) })
		c.transitionIndex = 0
	} else {
		// 普通递增流沿用原单向快路径，周期右端点归下一个周期。
		for c.workIndex < len(c.works) && !at.Before(c.works[c.workIndex].queryEnd) {
			c.workIndex++
			c.transitionIndex = 0
		}
	}
	// 空洞或末尾只跳过本行；之后的倒序事件仍可重新定位，不能提前结束整个流。
	if c.workIndex >= len(c.works) {
		return nil, nil
	}
	work := &c.works[c.workIndex]
	if at.Before(work.queryStart) || !at.Before(work.queryEnd) {
		return nil, nil
	}
	transitions := work.record.Transitions
	if backwards {
		c.transitionIndex = sort.Search(len(transitions), func(index int) bool { return !at.After(transitions[index].IntervalEndedAt) })
	} else {
		// 相同 instant 的不同 ID 仍累计到同一个左开右闭区间。
		for c.transitionIndex < len(transitions) && at.After(transitions[c.transitionIndex].IntervalEndedAt) {
			c.transitionIndex++
		}
	}
	if c.transitionIndex >= len(transitions) {
		return work, nil
	}
	transition := &transitions[c.transitionIndex]
	if !at.After(transition.IntervalStartedAt) || at.After(transition.IntervalEndedAt) {
		return work, nil
	}
	return work, transition
}
