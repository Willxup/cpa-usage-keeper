package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	quotaCostRecalculationCycleBatchSize  = 64
	quotaCostRecalculationConflictRetries = 3
)

type quotaCostStaleEventRow struct {
	Provider  string
	AuthIndex string
	Timestamp codexQuotaEfficiencyEventTime
}

// markQuotaCyclesCostStaleInTx 在显式价格重算的同批 Writer 事务内，把受改价 OAuth 事件影响的周期标为 cost_stale=true。
func markQuotaCyclesCostStaleInTx(tx *gorm.DB, changedEventIDs []int64) error {
	if tx == nil || len(changedEventIDs) == 0 {
		return nil
	}

	timestampsByAccount := make(map[quotaHistoryProviderAuthKey][]time.Time)
	for start := 0; start < len(changedEventIDs); start += usageCostRecalculationIDChunk {
		end := min(start+usageCostRecalculationIDChunk, len(changedEventIDs))
		rows, err := tx.Table("usage_events").
			Select("provider, auth_index, +timestamp AS timestamp").
			Where("id IN ? AND auth_type = ? AND provider IN ? AND auth_index <> ''",
				changedEventIDs[start:end], "oauth", []string{"codex", "claude"}).
			Rows()
		if err != nil {
			return fmt.Errorf("load changed oauth usage events for quota cost stale: %w", err)
		}
		for rows.Next() {
			var row quotaCostStaleEventRow
			if err := rows.Scan(&row.Provider, &row.AuthIndex, &row.Timestamp); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan changed oauth usage event for quota cost stale: %w", err)
			}
			key := quotaHistoryProviderAuthKey{
				Provider:  strings.ToLower(strings.TrimSpace(row.Provider)),
				AuthIndex: strings.TrimSpace(row.AuthIndex),
			}
			if key.AuthIndex == "" {
				continue
			}
			timestampsByAccount[key] = append(timestampsByAccount[key], timeutil.NormalizeStorageTime(row.Timestamp.Time))
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate changed oauth usage events for quota cost stale: %w", err)
		}
		_ = rows.Close()
	}

	if len(timestampsByAccount) == 0 {
		return nil
	}

	affectedSet := make(map[int64]struct{})
	affectedCycleIDs := make([]int64, 0)
	for key, timestamps := range timestampsByAccount {
		minAt, maxAt := timestamps[0], timestamps[0]
		for _, ts := range timestamps[1:] {
			if ts.Before(minAt) {
				minAt = ts
			}
			if ts.After(maxAt) {
				maxAt = ts
			}
		}
		var candidates []entities.QuotaCycle
		if err := tx.Select("id, window_started_at, reset_at").
			Where("provider = ? AND auth_index = ? AND reset_at > ? AND window_started_at <= ?",
				key.Provider, key.AuthIndex,
				timeutil.FormatSortableStorageTime(minAt),
				timeutil.FormatSortableStorageTime(maxAt)).
			Find(&candidates).Error; err != nil {
			return fmt.Errorf("find quota cycles affected by pricing recalculation: %w", err)
		}
		for _, cycle := range candidates {
			if _, exists := affectedSet[cycle.ID]; exists {
				continue
			}
			for _, ts := range timestamps {
				if !ts.Before(cycle.WindowStartedAt) && ts.Before(cycle.ResetAt) {
					affectedSet[cycle.ID] = struct{}{}
					affectedCycleIDs = append(affectedCycleIDs, cycle.ID)
					break
				}
			}
		}
	}
	if len(affectedCycleIDs) == 0 {
		return nil
	}
	sort.Slice(affectedCycleIDs, func(i, j int) bool { return affectedCycleIDs[i] < affectedCycleIDs[j] })

	formattedNow := timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now()))
	for start := 0; start < len(affectedCycleIDs); start += quotaCycleUpdateIDBatchSize {
		end := min(start+quotaCycleUpdateIDBatchSize, len(affectedCycleIDs))
		if err := tx.Model(&entities.QuotaCycle{}).
			Where("id IN ?", affectedCycleIDs[start:end]).
			Updates(map[string]any{
				"cost_stale": true,
				"updated_at": formattedNow,
			}).Error; err != nil {
			return fmt.Errorf("mark quota cycles cost_stale: %w", err)
		}
	}
	return nil
}

// RecomputeCostStaleQuotaCycles 复用热表 Reader 重算逻辑，把所有 cost_stale=true 周期的 11 项统计对齐到 id <= maxEventID 并清零标记。
// 超出 90 天热表保留线的陈旧周期直接清标记跳过，绝不访问 usage_events_archive。
func RecomputeCostStaleQuotaCycles(ctx context.Context, db *gorm.DB, now time.Time, maxEventID int64) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("recompute cost stale quota cycles: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}

	recomputed := 0
	afterID := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return recomputed, err
		}
		var staleCycles []entities.QuotaCycle
		if err := db.WithContext(ctx).Clauses(dbresolver.Read).
			Where("cost_stale = ? AND id > ?", true, afterID).
			Order("id ASC").
			Limit(quotaCostRecalculationCycleBatchSize).
			Find(&staleCycles).Error; err != nil {
			return recomputed, fmt.Errorf("load cost_stale quota cycles: %w", err)
		}
		if len(staleCycles) == 0 {
			return recomputed, nil
		}
		for _, cycle := range staleCycles {
			applied, err := recomputeSingleCostStaleQuotaCycle(ctx, db, cycle, now, maxEventID)
			if err != nil {
				return recomputed, err
			}
			if applied {
				recomputed++
			}
		}
		afterID = staleCycles[len(staleCycles)-1].ID
		if len(staleCycles) < quotaCostRecalculationCycleBatchSize {
			return recomputed, nil
		}
	}
}

// recomputeSingleCostStaleQuotaCycle 对单个 cost_stale 周期执行重算与落库；
// 显式改价期间配额采样不暂停，若遇并发 boundary_version 冲突（applied == false），
// 重新按 id = ? AND cost_stale = true 读取最新周期最多重试 3 次，避免静默遗留 cost_stale = true。
func recomputeSingleCostStaleQuotaCycle(ctx context.Context, db *gorm.DB, initial entities.QuotaCycle, now time.Time, maxEventID int64) (bool, error) {
	current := initial
	for attempt := 0; attempt <= quotaCostRecalculationConflictRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if attempt > 0 {
			var latest entities.QuotaCycle
			err := db.WithContext(ctx).Clauses(dbresolver.Read).
				Where("id = ? AND cost_stale = ?", initial.ID, true).
				Take(&latest).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 周期已被删除或不再 cost_stale，视为完成。
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("reload cost_stale quota cycle %d on conflict retry %d: %w", initial.ID, attempt, err)
			}
			current = latest
		}

		computed, err := ComputeQuotaCyclePersistedUsage(ctx, db, current, now, maxEventID)
		if err != nil {
			return false, err
		}
		applied, err := ApplyComputedQuotaCycleUsage(ctx, db, computed, now, true)
		if err != nil {
			return false, err
		}
		if applied {
			return true, nil
		}
	}

	// 达到重试上限仍未落库时，最后核对一次周期是否已被删除或清标记，避免把正常删除误判为持续冲突。
	var remaining entities.QuotaCycle
	checkErr := db.WithContext(ctx).Clauses(dbresolver.Read).
		Select("id").
		Where("id = ? AND cost_stale = ?", initial.ID, true).
		Take(&remaining).Error
	if errors.Is(checkErr, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if checkErr != nil {
		return false, fmt.Errorf("verify cost_stale quota cycle %d after conflict retries: %w", initial.ID, checkErr)
	}
	return false, fmt.Errorf("recompute cost_stale quota cycle %d: boundary_version conflict persisted after %d retries", initial.ID, quotaCostRecalculationConflictRetries)
}
