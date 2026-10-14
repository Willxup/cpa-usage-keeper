package dto

import "time"

// UsageWindowSummaryRecord 是最近事件缓存中单个短窗口的精确汇总。
// 所有窗口共享同一个右边界 now，WindowSeconds 描述窗口长度。
type UsageWindowSummaryRecord struct {
	WindowSeconds       int64
	WindowStart         time.Time
	WindowEnd           time.Time
	Requests            int64
	Failures            int64
	TokenRequests       int64
	CacheHitRequests    int64
	TotalTokens         int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             float64
	CostAvailable       bool
}
