package quota

import (
	"context"
	"strings"
	"time"

	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
)

type quotaUsageWindowKey struct {
	start time.Time
	end   time.Time
}

type quotaGroupedUsageWindowResult struct {
	stats  repository.UsageWindowGroupedStats
	failed bool
}

type usageWindowStatsProvider interface {
	SumByAuthIndex(context.Context, string, time.Time, *time.Time) (repository.UsageWindowStats, error)
	SumGroupsByAuthIndex(context.Context, string, time.Time, *time.Time, repository.UsageWindowStatsGrouper) (repository.UsageWindowGroupedStats, error)
}

func (s *Service) attachWindowUsageStats(ctx context.Context, authIndex string, response CheckResponse, now time.Time) CheckResponse {
	if s == nil {
		return response
	}
	calculator, err := repository.NewUsageWindowStatsCalculator(ctx, s.db, s.pricing.NewResolver())
	if err != nil {
		return response
	}
	return s.attachWindowUsageStatsWithProvider(ctx, authIndex, response, now, calculator)
}

func (s *Service) attachWindowUsageStatsWithProvider(ctx context.Context, authIndex string, response CheckResponse, now time.Time, statsProvider usageWindowStatsProvider) CheckResponse {
	// quota 为空时没有可补充的窗口用量，直接返回原响应。
	if len(response.Quota) == 0 || statsProvider == nil {
		// 返回原响应，避免后续 map 和数据库查询开销。
		return response
	}
	// 同一个 quota response 可能包含多个相同窗口，按 start/end 去重后再查询数据库。
	statsByWindow := make(map[quotaUsageWindowKey]repository.UsageWindowStats)
	groupedStatsByWindow := make(map[quotaUsageWindowKey]quotaGroupedUsageWindowResult)
	// 遍历每一条 quota row，只对没有完整 provider 用量的普通窗口补 token/cost。
	for index := range response.Quota {
		if groupKey, ok := antigravityQuotaUsageGroupKey(response.Quota[index]); ok {
			// 完整 provider pair 仍优先；Antigravity 当前不返回该字段，但保持统一信任规则。
			if response.Quota[index].WindowUsageTokens != nil && response.Quota[index].WindowUsageCost != nil {
				continue
			}
			response.Quota[index].WindowUsageTokens = nil
			response.Quota[index].WindowUsageCost = nil
			windowStart, windowEnd, windowOK := quotaRowUsageWindow(response.Quota[index], now)
			if !windowOK {
				continue
			}
			key := quotaUsageWindowKey{start: windowStart, end: windowEnd}
			result, cached := groupedStatsByWindow[key]
			if !cached {
				var err error
				result.stats, err = statsProvider.SumGroupsByAuthIndex(ctx, authIndex, windowStart, &windowEnd, antigravityUsageGroupForModel)
				result.failed = err != nil
				// 失败结果也按窗口缓存，避免同一响应内重复查询后只发布部分模型组。
				groupedStatsByWindow[key] = result
			}
			if result.failed {
				continue
			}
			grouped := result.stats
			// 任一正 Token 模型无法归组时，整个窗口都不能发布可能低估的分组统计。
			if !grouped.Complete {
				continue
			}
			stats, exists := grouped.Groups[groupKey]
			if !exists {
				// 已知组在完整查询中没有事件时是可靠的零用量，而不是缺失结果。
				stats.CostAvailable = true
			}
			if !stats.CostAvailable {
				continue
			}
			tokens := stats.Tokens
			cost := stats.Cost
			response.Quota[index].WindowUsageTokens = &tokens
			response.Quota[index].WindowUsageCost = &cost
			continue
		}
		// Pro 等上游可能已经返回窗口 token/cost；非 window scope 不用本地 auth 级统计兜底。
		if !shouldBackfillWindowUsageStats(response.Quota[index]) {
			// 保留 provider 原始字段，避免覆盖 additional/code_review 等专项限额。
			continue
		}
		// provider pair 不完整时先丢弃单边字段，避免 fallback 失败后输出不同口径的半套数据。
		response.Quota[index].WindowUsageTokens = nil
		response.Quota[index].WindowUsageCost = nil
		// 根据 reset_at 和 window.seconds 计算该 row 对应的统计窗口。
		windowStart, windowEnd, ok := quotaRowUsageWindow(response.Quota[index], now)
		// 没有明确窗口或 reset_at 无法解析时跳过该 row。
		if !ok {
			// 跳过后该 row 不展示窗口 token/cost。
			continue
		}
		// start/end 组成窗口缓存 key，避免同一响应内重复查同一窗口。
		key := quotaUsageWindowKey{start: windowStart, end: windowEnd}
		// 先尝试复用本次响应内已经查询过的窗口统计。
		stats, ok := statsByWindow[key]
		// 没有缓存时才真正查询 repository。
		if !ok {
			// repository 内部会按窗口长度选择 raw group by 或 hourly rollup。
			var err error
			// 调用窗口统计查询，end 使用半开区间避免重复累计边界事件。
			stats, err = statsProvider.SumByAuthIndex(ctx, authIndex, windowStart, &windowEnd)
			// 统计失败不影响 quota 主结果，只跳过当前窗口用量展示。
			if err != nil {
				// 当前 row 不写 token/cost，继续处理其它 row。
				continue
			}
			// 把查询结果放入本次响应缓存，供相同窗口的其它 row 复用。
			statsByWindow[key] = stats
		}
		// 复制 tokens/cost 值，确保指针指向当前 row 独立变量。
		tokens := stats.Tokens
		cost := stats.Cost
		// token 和 cost 必须来自同一统计口径；provider pair 不完整时整对使用本地统计。
		response.Quota[index].WindowUsageTokens = &tokens
		response.Quota[index].WindowUsageCost = &cost
	}
	// 返回已经补充窗口用量的 quota 响应。
	return response
}

func antigravityQuotaUsageGroupKey(row QuotaRow) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(row.Scope), "quota_group") {
		return "", false
	}
	if row.Window == nil || row.Window.Seconds == nil {
		return "", false
	}
	switch *row.Window.Seconds {
	case quotaWindowFiveHourSeconds, quotaWindowSevenDaySeconds:
	default:
		return "", false
	}
	groupKey := strings.TrimSpace(row.GroupKey)
	if !strings.HasPrefix(strings.TrimSpace(row.Key), "bucket."+groupKey+".") {
		return "", false
	}
	switch groupKey {
	case antigravityGeminiGroupKey, antigravityClaudeGPTGroupKey:
		return groupKey, true
	default:
		return "", false
	}
}

func antigravityUsageGroupForModel(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "gemini-"):
		return antigravityGeminiGroupKey, true
	case strings.HasPrefix(normalized, "claude-"), strings.HasPrefix(normalized, "gpt-"):
		return antigravityClaudeGPTGroupKey, true
	default:
		return "", false
	}
}

func shouldBackfillWindowUsageStats(row QuotaRow) bool {
	// provider 已给完整窗口用量时直接信任上游，不再用 usage_events 覆盖。
	if row.WindowUsageTokens != nil && row.WindowUsageCost != nil {
		return false
	}
	// 普通窗口和 xAI canonical Weekly 都能安全映射到当前 auth_index 的明确时间范围。
	ordinaryWindow := strings.EqualFold(strings.TrimSpace(row.Scope), "window")
	if !ordinaryWindow && !isXAIWeeklyUsageWindowQuotaRow(row) {
		return false
	}
	if row.Window == nil || row.Window.Seconds == nil {
		return false
	}
	switch *row.Window.Seconds {
	case quotaWindowFiveHourSeconds, quotaWindowSevenDaySeconds, quotaWindowThirtyDaySeconds, quotaWindowAverageMonthSeconds:
		return true
	default:
		return false
	}
}

func quotaRowUsageWindow(row QuotaRow, now time.Time) (time.Time, time.Time, bool) {
	// window.seconds 是计算窗口起点的必要条件，没有明确窗口长度就不能补 token/cost。
	if row.Window == nil || row.Window.Seconds == nil || *row.Window.Seconds <= 0 {
		// 返回 false 表示该 row 不参与窗口 token/cost 计算。
		return time.Time{}, time.Time{}, false
	}
	// now 归一化成项目存储时间，作为 reset_after 推导和当前未结束窗口的实际查询终点。
	now = timeutil.NormalizeStorageTime(now)
	// resetAt 承接 provider 返回的绝对 reset_at，或由 reset_after_seconds 推导出的绝对重置时间。
	var resetAt time.Time
	// reset_at 有值时优先使用绝对时间，避免本地 now 和上游响应时间之间的网络延迟影响窗口。
	if row.ResetAt != "" {
		// provider 返回的 reset_at 当前统一按 RFC3339 解析。
		parsedResetAt, err := timeutil.ParseStorageTime(row.ResetAt)
		// reset_at 解析失败时不能安全计算窗口，直接跳过。
		if err != nil {
			// 返回 false 表示该 row 不参与窗口 token/cost 计算。
			return time.Time{}, time.Time{}, false
		}
		// reset_at 归一化成项目存储时间，保证与 usage_events.timestamp 比较一致。
		resetAt = timeutil.NormalizeStorageTime(parsedResetAt)
	} else if row.ResetAfterSeconds != nil && *row.ResetAfterSeconds >= 0 {
		// 只有相对 reset_after_seconds 可用时，用当前刷新时间推导本轮 quota 的重置点。
		resetAt = now.Add(time.Duration(*row.ResetAfterSeconds) * time.Second)
	} else {
		// 既没有 reset_at 也没有 reset_after_seconds 时无法定位窗口结束点。
		return time.Time{}, time.Time{}, false
	}
	// windowStart 是 reset_at 往前减去明确窗口秒数。
	windowStart := resetAt.Add(-time.Duration(*row.Window.Seconds) * time.Second)
	// 当前时间早于 reset_at 时，窗口还没结束，查询终点用 now 避免扫描未来空小时。
	if now.Before(resetAt) {
		// 返回 [windowStart, now) 作为当前窗口统计范围。
		return windowStart, now, true
	}
	// 当前时间已经到达或超过 reset_at 时，查询终点用 reset_at 锁定旧窗口。
	return windowStart, resetAt, true
}
