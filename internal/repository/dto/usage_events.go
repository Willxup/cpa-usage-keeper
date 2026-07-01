package dto

import "time"

// UsageEventsPageRecord 是 usage events 列表的仓储查询结果。
type UsageEventsPageRecord struct {
	Events     []UsageEventRecord
	Models     []string
	TotalCount int64
	Page       int
	PageSize   int
	TotalPages int
}

// UsageEventFilterOptionsRecord 是 usage events 筛选项的仓储查询结果。
type UsageEventFilterOptionsRecord struct {
	Models []string
}

// UsageEventRecord 是单条 usage event 的查询结果。
type UsageEventRecord struct {
	ID                  int64
	Timestamp           time.Time
	APIGroupKey         string
	Model               string
	ModelAlias          string
	ReasoningEffort     string
	ServiceTier         string
	ExecutorType        string
	Endpoint            string
	AuthType            string
	Provider            string
	Source              string
	AuthIndex           string
	Failed              bool
	LatencyMS           int64
	TTFTMS              *int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CostUSD             float64
	CostAvailable       bool
	PricingStyle        string
}
