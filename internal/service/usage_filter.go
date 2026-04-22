package service

import "time"

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
