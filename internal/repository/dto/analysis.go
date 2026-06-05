package dto

import "time"

type AnalysisGranularity string

const (
	AnalysisGranularityHourly AnalysisGranularity = "hourly"
	AnalysisGranularityDaily  AnalysisGranularity = "daily"
)

type AnalysisTokenUsageBucketRecord struct {
	Bucket          time.Time
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	TotalTokens     int64
	Requests        int64
	CostUSD         float64
	CostAvailable   bool
}

type AnalysisCompositionRecord struct {
	Key             string
	Label           string
	TotalTokens     int64
	Requests        int64
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	CostUSD         float64
	CostAvailable   bool
}

type AnalysisHeatmapRecord struct {
	APIKey          string
	Model           string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	TotalTokens     int64
	Requests        int64
	CostUSD         float64
	CostAvailable   bool
}

type AnalysisCostBreakdownRecord struct {
	InputCostUSD  float64
	OutputCostUSD float64
	CachedCostUSD float64
	TotalCostUSD  float64
	CostAvailable bool
}

type AnalysisModelEfficiencyRecord struct {
	Model                  string
	Requests               int64
	InputTokens            int64
	OutputTokens           int64
	CachedTokens           int64
	ReasoningTokens        int64
	TotalTokens            int64
	CostUSD                float64
	CostAvailable          bool
	CostPerRequestUSD      float64
	OutputTokensPerRequest float64
	CacheRate              float64
}

type AnalysisRecord struct {
	Granularity           AnalysisGranularity
	RangeStart            *time.Time
	RangeEnd              *time.Time
	TokenUsage            []AnalysisTokenUsageBucketRecord
	APIKeyComposition     []AnalysisCompositionRecord
	ModelComposition      []AnalysisCompositionRecord
	AuthFilesComposition  []AnalysisCompositionRecord
	AIProviderComposition []AnalysisCompositionRecord
	Heatmap               []AnalysisHeatmapRecord
	CostBreakdown         AnalysisCostBreakdownRecord
	ModelEfficiency       []AnalysisModelEfficiencyRecord
}
