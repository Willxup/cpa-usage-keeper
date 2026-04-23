package repository

import "time"

type UsageQueryFilter struct {
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

type UsageCredentialStatRecord struct {
	Source       string
	AuthIndex    string
	Failed       bool
	RequestCount int64
}

type UsageAnalysisModelStatRecord struct {
	Model              string
	TotalRequests      int64
	SuccessCount       int64
	FailureCount       int64
	InputTokens        int64
	OutputTokens       int64
	ReasoningTokens    int64
	CachedTokens       int64
	TotalTokens        int64
	TotalLatencyMS     int64
	LatencySampleCount int64
}

type UsageAnalysisAPIStatRecord struct {
	APIGroupKey     string
	DisplayName     string
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	Models          []UsageAnalysisModelStatRecord `gorm:"-"`
}
