package dto

import (
	"time"

	repodto "cpa-usage-keeper/internal/repository/dto"
)

const DefaultUsageEventsLimit = 100

// UsageFilter 是服务层的 usage 查询条件。
type UsageFilter struct {
	Range     string
	StartTime *time.Time
	EndTime   *time.Time
	// QueryNow 仅供内部调用固定仓储层当前时刻，API 层不需要显式传这个值。
	QueryNow *time.Time
	// RealtimeWindow 控制 Overview 实时图表短窗口，独立于页面主查询范围。
	RealtimeWindow  string
	RealtimeEndTime *time.Time
	Limit           int
	Page            int
	PageSize        int
	Offset          int
	Model           string
	Source          string
	AuthIndex       string
	APIKeyID        string
	Result          string
}

// UsageEventsPage 是 usage events 列表的服务层结果。
type UsageEventsPage struct {
	Events     []UsageEventRecord
	Models     []string
	TotalCount int64
	Page       int
	PageSize   int
	TotalPages int
}

// UsageEventFilterOptions 是 usage events 筛选项的服务层结果。
type UsageEventFilterOptions struct {
	Models []string
}

// UsageEventRecord 是单条 usage event 的服务层结果。
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

// UsageOverviewSummary 是 overview summary 的服务层结果。
type UsageOverviewSummary struct {
	RequestCount          int64
	TokenCount            int64
	WindowMinutes         int64
	RPM                   float64
	TPM                   float64
	TotalCost             float64
	CostAvailable         bool
	InputTokens           int64
	CachedTokens          int64
	ReasoningTokens       int64
	DailyAverageRequests  *float64
	DailyAverageTokens    *float64
	DailyAverageCost      *float64
	DailyAverageRangeDays *float64
}

// UsageOverviewSeries 是 overview series 的服务层结果。
type UsageOverviewSeries struct {
	Requests  map[string]int64
	Tokens    map[string]int64
	RPM       map[string]float64
	TPM       map[string]float64
	Cost      map[string]float64
	CacheRate map[string]*float64
}

// UsageOverviewHealthBlock 是 overview health 的单个时间块。
type UsageOverviewHealthBlock struct {
	StartTime time.Time
	EndTime   time.Time
	Success   int64
	Failure   int64
	Rate      float64
}

// UsageOverviewHealth 是 overview health 的聚合结果。
type UsageOverviewHealth struct {
	TotalSuccess  int64
	TotalFailure  int64
	SuccessRate   float64
	Rows          int
	Columns       int
	BucketSeconds int64
	WindowStart   time.Time
	WindowEnd     time.Time
	BlockDetails  []UsageOverviewHealthBlock
}

// RealtimeTokenVelocityPoint 是 Overview token 速度图的单个短窗口桶。
type RealtimeTokenVelocityPoint struct {
	Bucket          string
	TokensPerMinute float64
	Tokens          int64
	CostUSD         *float64
}

// RealtimeResponseLevelPoint 是 Overview 响应水平图的单个短窗口桶。
type RealtimeResponseLevelPoint struct {
	Bucket       string
	TTFTP50MS    *int64
	TTFTP95MS    *int64
	LatencyP50MS *int64
	LatencyP95MS *int64
}

// RealtimeResponseAveragePoint 是响应分布图的一条平均线点。
type RealtimeResponseAveragePoint struct {
	Bucket string
	AvgMS  *float64
}

// RealtimeResponseParticle 是响应分布图的一个聚合粒子点。
type RealtimeResponseParticle struct {
	Bucket    string
	Timestamp string
	MS        int64
	Count     int64
}

// RealtimeResponseDistributionSeries 是单个响应指标的平均线和粒子分布。
type RealtimeResponseDistributionSeries struct {
	AverageLine    []RealtimeResponseAveragePoint
	Particles      []RealtimeResponseParticle
	TotalParticles int64
	Sampled        bool
	MaxParticles   int
}

// RealtimeResponseDistribution 是 TTFT 和 Latency 的实时响应分布。
type RealtimeResponseDistribution struct {
	TTFT    RealtimeResponseDistributionSeries
	Latency RealtimeResponseDistributionSeries
}

// RealtimeUsageTopItem 是 Overview 当前使用 Top 列表项。
type RealtimeUsageTopItem struct {
	Key      string
	Label    string
	Tokens   int64
	Requests int64
	CostUSD  *float64
	Share    float64
}

// RealtimeCurrentUsage 是 Overview 当前使用按维度聚合的 Top 列表。
type RealtimeCurrentUsage struct {
	Models      []RealtimeUsageTopItem
	APIKeys     []RealtimeUsageTopItem
	AuthFiles   []RealtimeUsageTopItem
	AIProviders []RealtimeUsageTopItem
}

// RealtimeRequestLevelPoint 是 Overview 请求水平图的单个短窗口桶。
type RealtimeRequestLevelPoint struct {
	Bucket            string
	RequestsPerMinute float64
	Requests          int64
}

// RealtimeCacheLevelPoint 是 Overview 缓存水平图的单个短窗口桶。
type RealtimeCacheLevelPoint struct {
	Bucket       string
	CacheRate    *float64
	CachedTokens int64
	InputTokens  int64
}

// UsageOverviewRealtime 是 Overview 页面实时图表区使用的数据块。
type UsageOverviewRealtime struct {
	Window               string
	BucketSeconds        int64
	WindowStart          time.Time
	WindowEnd            time.Time
	TokenVelocity        []RealtimeTokenVelocityPoint
	ResponseLevel        []RealtimeResponseLevelPoint
	ResponseDistribution RealtimeResponseDistribution
	CurrentUsage         RealtimeCurrentUsage
	RequestLevel         []RealtimeRequestLevelPoint
	CacheLevel           []RealtimeCacheLevelPoint
}

// UsageOverviewSnapshot 是 overview 的服务层结果。
type UsageOverviewSnapshot struct {
	Usage   *repodto.StatisticsSnapshot
	Summary UsageOverviewSummary
	Series  UsageOverviewSeries
	Health  UsageOverviewHealth
}
