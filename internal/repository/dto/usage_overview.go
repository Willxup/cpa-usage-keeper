package dto

import "time"

// UsageOverviewSummaryRecord 是 overview 的 summary 聚合结果。
type UsageOverviewSummaryRecord struct {
	RequestCount          int64
	TokenCount            int64
	WindowMinutes         int64
	RPM                   float64
	TPM                   float64
	TotalCost             float64
	CostAvailable         bool
	InputTokens           int64
	CacheReadTokens       int64
	CacheCreationTokens   int64
	ReasoningTokens       int64
	DailyAverageRequests  *float64
	DailyAverageTokens    *float64
	DailyAverageCost      *float64
	DailyAverageRangeDays *float64
}

// UsageOverviewSeriesRecord 是 overview 的 series 聚合结果。
type UsageOverviewSeriesRecord struct {
	Requests                 map[string]int64
	Tokens                   map[string]int64
	RPM                      map[string]float64
	TPM                      map[string]float64
	Cost                     map[string]float64
	CacheReadRate            map[string]*float64
	CacheReadRateInputTokens map[string]int64
	CacheReadRateReadTokens  map[string]int64
}

// RealtimeTokenVelocityPointRecord 是 Overview token 速度图的单个短窗口桶。
type RealtimeTokenVelocityPointRecord struct {
	Bucket          string
	TokensPerMinute float64
	Tokens          int64
	CostUSD         *float64
}

// RealtimeLatencyScatterRecord 保留同一请求的 TTFT/总耗时配对；摘要覆盖全部可见请求。
type RealtimeLatencyScatterRecord struct {
	Points       []RealtimeLatencyScatterPointRecord
	TotalPoints  int64
	P95TTFTMS    int64
	P95LatencyMS int64
	MaxTTFTMS    int64
	MaxLatencyMS int64
}

type RealtimeLatencyScatterPointRecord struct {
	TTFTMS    int64
	LatencyMS int64
}

// RealtimeUsageTopItemRecord 是 Overview 当前使用 Top 列表项。
type RealtimeUsageTopItemRecord struct {
	Key      string
	Label    string
	Tokens   int64
	Requests int64
	CostUSD  *float64
	Share    float64
}

// RealtimeCurrentUsageRecord 是 Overview 当前使用按维度聚合的 Top 列表。
type RealtimeCurrentUsageRecord struct {
	Models      []RealtimeUsageTopItemRecord
	APIKeys     []RealtimeUsageTopItemRecord
	AuthFiles   []RealtimeUsageTopItemRecord
	AIProviders []RealtimeUsageTopItemRecord
}

// UsageOverviewRealtimeRecord 是 Overview 页面实时图表区使用的数据块。
type UsageOverviewRealtimeRecord struct {
	Insights       RealtimeInsightsRecord
	Window         string
	BucketSeconds  int64
	WindowStart    time.Time
	WindowEnd      time.Time
	TokenVelocity  []RealtimeTokenVelocityPointRecord
	LatencyScatter RealtimeLatencyScatterRecord
	CurrentUsage   RealtimeCurrentUsageRecord
	RequestLevel   []RealtimeRequestLevelPointRecord
	CacheLevel     []RealtimeCacheLevelPointRecord
}

// RealtimeRequestLevelPointRecord 是 Overview 请求水平图的单个短窗口桶。
type RealtimeRequestLevelPointRecord struct {
	Bucket            string
	RequestsPerMinute float64
	Requests          int64
}

// RealtimeCacheLevelPointRecord 是 Overview 缓存水平图的单个短窗口桶。
type RealtimeCacheLevelPointRecord struct {
	Bucket              string
	CacheReadRate       *float64
	CacheReadTokens     int64
	CacheCreationTokens int64
	InputTokens         int64
}

// UsageOverviewRecord 是仓储层的完整 usage overview 结果。
type UsageOverviewRecord struct {
	Comparisons *UsageOverviewComparisonsRecord
	Usage       *StatisticsSnapshot
	Summary     UsageOverviewSummaryRecord
	Series      UsageOverviewSeriesRecord
}

// UsageComparisonItemRecord 与顶部 Overview 共用请求、Token 和动态计费口径。
type UsageComparisonItemRecord struct {
	Key                 string
	Label               string
	Requests            int64
	Failures            int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	TotalTokens         int64
	CostUSD             float64
	CostAvailable       bool
}

// UsageOverviewComparisonsRecord 在压缩汇总行与边界事件遍历中按维度累计。
type UsageOverviewComparisonsRecord struct {
	Models      map[string]*UsageComparisonItemRecord
	APIKeys     map[string]*UsageComparisonItemRecord
	AuthFiles   map[string]*UsageComparisonItemRecord
	AIProviders map[string]*UsageComparisonItemRecord
}

// RealtimeWindowSummaryRecord 是选定可见短窗的非重叠总量，排除平滑预热段。
type RealtimeWindowSummaryRecord struct {
	Requests            int64
	Failures            int64
	TokenRequests       int64
	CachedRequests      int64
	TotalTokens         int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             float64
	CostAvailable       bool
}

type RealtimeOutcomePointRecord struct {
	Bucket   string
	Requests int64
	Failures int64
}

type RealtimeInsightsRecord struct {
	Summary  RealtimeWindowSummaryRecord
	Outcomes []RealtimeOutcomePointRecord
}
