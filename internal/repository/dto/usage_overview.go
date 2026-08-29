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

// RealtimeResponseLevelPointRecord 是 Overview 响应水平图的单个短窗口桶。
type RealtimeResponseLevelPointRecord struct {
	Bucket       string
	TTFTP50MS    *int64
	TTFTP95MS    *int64
	LatencyP50MS *int64
	LatencyP95MS *int64
}

// RealtimeResponseAveragePointRecord 是响应分布图的一条平均线点。
type RealtimeResponseAveragePointRecord struct {
	Bucket string
	AvgMS  *float64
}

// RealtimeResponseParticleRecord 是响应分布图的一个聚合粒子点。
type RealtimeResponseParticleRecord struct {
	Bucket    string
	Timestamp string
	MS        int64
	Count     int64
}

// RealtimeResponseDistributionSeriesRecord 是单个响应指标的平均线和粒子分布。
type RealtimeResponseDistributionSeriesRecord struct {
	AverageLine    []RealtimeResponseAveragePointRecord
	Particles      []RealtimeResponseParticleRecord
	TotalParticles int64
	Sampled        bool
	MaxParticles   int
}

// RealtimeResponseDistributionRecord 是 TTFT 和 Latency 的实时响应分布。
type RealtimeResponseDistributionRecord struct {
	TTFT    RealtimeResponseDistributionSeriesRecord
	Latency RealtimeResponseDistributionSeriesRecord
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
	Window               string
	BucketSeconds        int64
	WindowStart          time.Time
	WindowEnd            time.Time
	TokenVelocity        []RealtimeTokenVelocityPointRecord
	ResponseLevel        []RealtimeResponseLevelPointRecord
	ResponseDistribution RealtimeResponseDistributionRecord
	CurrentUsage         RealtimeCurrentUsageRecord
	RequestLevel         []RealtimeRequestLevelPointRecord
	CacheLevel           []RealtimeCacheLevelPointRecord
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
	Usage   *StatisticsSnapshot
	Summary UsageOverviewSummaryRecord
	Series  UsageOverviewSeriesRecord
}
