package dto

import "time"

// UsageSummaryWindow 是单个短窗口的汇总结果（服务层）。
type UsageSummaryWindow struct {
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
