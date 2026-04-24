package service

import (
	"time"

	"cpa-usage-keeper/internal/cpa"
)

type UsageFilter struct {
	Range     string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

const DefaultUsageEventsLimit = 500

type UsageEventRecord struct {
	Timestamp       time.Time
	APIGroupKey     string
	Model           string
	Source          string
	AuthIndex       string
	Failed          bool
	LatencyMS       int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
}

type UsageCredentialStat struct {
	Source       string
	AuthIndex    string
	Failed       bool
	RequestCount int64
}

type UsageAnalysisModelStat struct {
	Model              string
	TotalRequests      int64
	SuccessCount       int64
	FailureCount       int64
	TotalTokens        int64
	InputTokens        int64
	OutputTokens       int64
	ReasoningTokens    int64
	CachedTokens       int64
	TotalLatencyMS     int64
	LatencySampleCount int64
}

type UsageAnalysisAPIStat struct {
	APIKey          string
	DisplayName     string
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	TotalTokens     int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	Models          []UsageAnalysisModelStat
}

type UsageAnalysisSnapshot struct {
	APIs   []UsageAnalysisAPIStat
	Models []UsageAnalysisModelStat
}

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

type UsageOverviewHealthBlock struct {
	StartTime time.Time
	EndTime   time.Time
	Success   int64
	Failure   int64
	Rate      float64
}

type UsageOverviewHealth struct {
	TotalSuccess int64
	TotalFailure int64
	SuccessRate  float64
	BlockDetails []UsageOverviewHealthBlock
}

type UsageOverviewSnapshot struct {
	Usage        *cpa.StatisticsSnapshot
	Summary      UsageOverviewSummary
	Series       UsageOverviewSeries
	HourlySeries UsageOverviewSeries
	DailySeries  UsageOverviewSeries
	Health       UsageOverviewHealth
}
