package dto

import "time"

// UsageOverviewSummaryRecord 是 overview 的 summary 聚合结果。
type UsageOverviewSummaryRecord struct {
	RequestCount       int64
	TokenCount         int64
	WindowMinutes      int64
	RPM                float64
	TPM                float64
	TotalCost          float64
	CostAvailable      bool
	CachedTokens       int64
	ReasoningTokens    int64
	CacheHitBaseTokens int64
	CacheHitRate       float64
}

// UsageOverviewSeriesRecord 是 overview 的 series 聚合结果。
type UsageOverviewSeriesRecord struct {
	Requests           map[string]int64
	Tokens             map[string]int64
	RPM                map[string]float64
	TPM                map[string]float64
	Cost               map[string]float64
	InputTokens        map[string]int64
	OutputTokens       map[string]int64
	CachedTokens       map[string]int64
	ReasoningTokens    map[string]int64
	CacheHitBaseTokens map[string]int64
	CacheHitRate       map[string]float64
	Models             map[string]UsageOverviewSeriesRecord
}

// UsageOverviewHealthBlockRecord 是 overview health 的单个时间块。
type UsageOverviewHealthBlockRecord struct {
	StartTime time.Time
	EndTime   time.Time
	Success   int64
	Failure   int64
	Rate      float64
}

// UsageOverviewHealthRecord 是 overview health 的聚合结果。
type UsageOverviewHealthRecord struct {
	TotalSuccess  int64
	TotalFailure  int64
	SuccessRate   float64
	Rows          int
	Columns       int
	BucketSeconds int64
	WindowStart   time.Time
	WindowEnd     time.Time
	BlockDetails  []UsageOverviewHealthBlockRecord
}

// UsageOverviewRecord 是仓储层的完整 usage overview 结果。
type UsageOverviewRecord struct {
	Usage        *StatisticsSnapshot
	Summary      UsageOverviewSummaryRecord
	Series       UsageOverviewSeriesRecord
	HourlySeries UsageOverviewSeriesRecord
	DailySeries  UsageOverviewSeriesRecord
	Health       UsageOverviewHealthRecord
}
