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
	Limit     int
	Page      int
	PageSize  int
	Offset    int
	Model     string
	Source    string
	AuthIndex string
	APIKeyID  string
	Result    string
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
	ReasoningEffort     string
	AuthType            string
	Provider            string
	Source              string
	AuthIndex           string
	Failed              bool
	LatencyMS           int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// UsageOverviewSummary 是 overview summary 的服务层结果。
type UsageOverviewSummary struct {
	RequestCount    int64
	TokenCount      int64
	WindowMinutes   int64
	RPM             float64
	TPM             float64
	TotalCost       float64
	CostAvailable   bool
	CachedTokens    int64
	ReasoningTokens int64
}

// UsageOverviewSeries 是 overview series 的服务层结果。
type UsageOverviewSeries struct {
	Requests        map[string]int64
	Tokens          map[string]int64
	RPM             map[string]float64
	TPM             map[string]float64
	Cost            map[string]float64
	InputTokens     map[string]int64
	OutputTokens    map[string]int64
	CachedTokens    map[string]int64
	ReasoningTokens map[string]int64
	Models          map[string]UsageOverviewSeries
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

// UsageOverviewSnapshot 是 overview 的服务层结果。
type UsageOverviewSnapshot struct {
	Usage        *repodto.StatisticsSnapshot
	Summary      UsageOverviewSummary
	Series       UsageOverviewSeries
	HourlySeries UsageOverviewSeries
	DailySeries  UsageOverviewSeries
	Health       UsageOverviewHealth
}
