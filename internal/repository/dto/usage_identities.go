package dto

import "time"

// UsageIdentityStatsDelta 是 usage identity 聚合统计的仓储层扫描结果。
type UsageIdentityStatsDelta struct {
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	FirstUsedAt     *time.Time
	LastUsedAt      *time.Time
	MaxUsageEventID int64
}
