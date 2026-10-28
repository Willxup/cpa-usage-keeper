package dto

import "time"

type AnalysisGranularity string

const (
	AnalysisGranularityHourly AnalysisGranularity = "hourly"
	AnalysisGranularityDaily  AnalysisGranularity = "daily"
)

type AnalysisTokenUsageBucket struct {
	Bucket              time.Time
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	TotalTokens         int64
	Requests            int64
	CostUSD             float64
	CostAvailable       bool
}

type AnalysisModelUsage struct {
	Bucket      time.Time
	Model       string
	TotalTokens int64
	Requests    int64
}

// AnalysisModelSpeed 保存跨源桶的 sum/count，显示桶在仓储层使用真实时间边界合并。
type AnalysisModelSpeed struct {
	RangeStart *time.Time
	RangeEnd   *time.Time
	Buckets    []AnalysisModelSpeedBucket
	Series     []AnalysisModelSpeedSeries
}

type AnalysisModelSpeedBucket struct {
	Start time.Time
	End   time.Time
}

type AnalysisModelSpeedSample struct {
	SpeedTPSSum      float64
	SpeedSampleCount int64
	MeanTPS          *float64
}

type AnalysisModelSpeedSeries struct {
	Model            string
	TotalTokens      int64
	SpeedSampleCount int64
	Samples          []AnalysisModelSpeedSample
}

type AnalysisCompositionItem struct {
	Key                 string
	Label               string
	TotalTokens         int64
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	CostUSD             float64
	CostAvailable       bool
}

type AnalysisHeatmapCell struct {
	APIKey              string
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	TotalTokens         int64
	Requests            int64
	CostUSD             float64
	CostAvailable       bool
}

// AnalysisCostSummary 只向服务调用方暴露已存总金额及其可用性。
type AnalysisCostSummary struct {
	TotalCostUSD  float64
	CostAvailable bool
}

type AnalysisModelEfficiencyItem struct {
	Model                  string
	Requests               int64
	InputTokens            int64
	OutputTokens           int64
	CacheReadTokens        int64
	CacheCreationTokens    int64
	ReasoningTokens        int64
	TotalTokens            int64
	CostUSD                float64
	CostAvailable          bool
	CostPerRequestUSD      float64
	OutputTokensPerRequest float64
	CacheReadRate          float64
}

type AnalysisLatencyPoint struct {
	TTFTMS    int64
	LatencyMS int64
}

type AnalysisLatencyDensityCell struct {
	TTFTMinMS    int64
	TTFTMaxMS    int64
	LatencyMinMS int64
	LatencyMaxMS int64
	Count        int64
	Intensity    float64
}

type AnalysisLatencyDiagnostics struct {
	Points       []AnalysisLatencyPoint
	Density      []AnalysisLatencyDensityCell
	TotalPoints  int64
	Sampled      bool
	P95TTFTMS    int64
	P95LatencyMS int64
	MaxTTFTMS    int64
	MaxLatencyMS int64
}

type AnalysisSnapshot struct {
	Granularity            AnalysisGranularity
	RangeStart             *time.Time
	RangeEnd               *time.Time
	TokenUsage             []AnalysisTokenUsageBucket
	ModelSpeed             AnalysisModelSpeed
	ModelUsage             []AnalysisModelUsage
	APIKeyComposition      []AnalysisCompositionItem
	ModelComposition       []AnalysisCompositionItem
	CredentialsComposition []AnalysisCompositionItem
	AIProviderComposition  []AnalysisCompositionItem
	Heatmap                []AnalysisHeatmapCell
	CostSummary            AnalysisCostSummary
	ModelEfficiency        []AnalysisModelEfficiencyItem
}
