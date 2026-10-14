package repository

import (
	"errors"
	"sort"
	"time"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// ErrRecentUsageCacheUnavailable 表示最近事件缓存不可用；调用方必须把它映射成明确的降级响应，
// 不能退化成空结果，否则会被误读成"窗口内没有流量"。
var ErrRecentUsageCacheUnavailable = errors.New("recent usage event cache is unavailable")

// SumUsageWindows 从最近事件缓存按给定窗口长度求和。
//
// windows 是窗口时长（必须为正），返回结果与入参一一对应、顺序一致。
// 所有窗口共享同一个右边界 now，因此一个事件会被计入所有"足够长"的窗口；
// 实现上先按窗口长度升序把事件落到最短的容纳窗口，再做一次前缀和补齐更长窗口，
// 单个事件摊还 O(log N)，窗口数量没有上限。
func SumUsageWindows(windows []time.Duration, now time.Time, recentCache *UsageRecentEventCache, apiGroupKey string, costResolver pricing.Resolver) ([]dto.UsageWindowSummaryRecord, error) {
	if len(windows) == 0 {
		return []dto.UsageWindowSummaryRecord{}, nil
	}
	if recentCache == nil {
		return nil, ErrRecentUsageCacheUnavailable
	}
	now = timeutil.NormalizeStorageTime(now)

	maxWindow := windows[0]
	for _, window := range windows {
		if window > maxWindow {
			maxWindow = window
		}
	}
	// 缓存只保留固定保留时长；请求超过覆盖范围必须明确失败，而不是静默少算。
	if maxWindow > recentCache.Window() {
		return nil, ErrRecentUsageCacheUnavailable
	}

	// 左边界取最长窗口，短窗口只需从更小的事件集合里筛选。
	events, ok := recentCache.Events(now.Add(-maxWindow), now, false, apiGroupKey)
	if !ok {
		return nil, ErrRecentUsageCacheUnavailable
	}

	// order 保存"排序后位置 -> 原始下标"；windows 的返回顺序由调用方决定。
	order := make([]int, len(windows))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(a, b int) bool { return windows[order[a]] < windows[order[b]] })

	sortedWindows := make([]time.Duration, len(windows))
	for position, source := range order {
		sortedWindows[position] = windows[source]
	}

	// acc 先按"落桶增量"累加，稍后再原地转换成前缀和。
	acc := make([]dto.UsageWindowSummaryRecord, len(windows))
	costUnavailable := make([]bool, len(windows))

	for _, cachedEvent := range events {
		event := recentUsageEventToEntity(cachedEvent)
		age := now.Sub(timeutil.NormalizeStorageTime(event.Timestamp))
		// 未来时间戳（时钟漂移）夹到 0，保证进入所有窗口而不是被丢弃。
		if age < 0 {
			age = 0
		}
		// 第一个"时长 >= age"的窗口是最短的容纳窗口；更长窗口由前缀和补齐。
		position := sort.Search(len(sortedWindows), func(i int) bool { return sortedWindows[i] >= age })
		if position >= len(sortedWindows) {
			continue
		}

		// 请求数包含成功和失败，与 realtime summary 口径一致。
		acc[position].Requests++
		if event.Failed {
			acc[position].Failures++
		}
		// 失败或没有 token 的请求不参与 token / 缓存 / 费用。
		if event.Failed || event.TotalTokens <= 0 {
			continue
		}

		costResult := costResolver.Calculate(UsageEventCostSubject(event))
		if !costResult.Available {
			costUnavailable[position] = true
		}
		acc[position].TokenRequests++
		if event.CacheReadTokens > 0 {
			acc[position].CacheHitRequests++
		}
		acc[position].TotalTokens += event.TotalTokens
		acc[position].InputTokens += event.InputTokens
		acc[position].OutputTokens += event.OutputTokens
		acc[position].ReasoningTokens += event.ReasoningTokens
		acc[position].CacheReadTokens += event.CacheReadTokens
		acc[position].CacheCreationTokens += event.CacheCreationTokens
		acc[position].CostUSD += costResult.Cost.TotalCostUSD
	}

	// 前缀和：sortedWindows 升序时，窗口 i 包含增量 0..i 的全部事件。
	for position := 1; position < len(acc); position++ {
		previous := acc[position-1]
		acc[position].Requests += previous.Requests
		acc[position].Failures += previous.Failures
		acc[position].TokenRequests += previous.TokenRequests
		acc[position].CacheHitRequests += previous.CacheHitRequests
		acc[position].TotalTokens += previous.TotalTokens
		acc[position].InputTokens += previous.InputTokens
		acc[position].OutputTokens += previous.OutputTokens
		acc[position].ReasoningTokens += previous.ReasoningTokens
		acc[position].CacheReadTokens += previous.CacheReadTokens
		acc[position].CacheCreationTokens += previous.CacheCreationTokens
		acc[position].CostUSD += previous.CostUSD
		costUnavailable[position] = costUnavailable[position] || costUnavailable[position-1]
	}

	result := make([]dto.UsageWindowSummaryRecord, len(windows))
	for position, source := range order {
		row := acc[position]
		row.WindowSeconds = int64(sortedWindows[position] / time.Second)
		row.WindowStart = now.Add(-sortedWindows[position])
		row.WindowEnd = now
		row.CostAvailable = !costUnavailable[position]
		result[source] = row
	}
	return result, nil
}
