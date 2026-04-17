package cpa

import "time"

type UsageExport struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exported_at"`
	Usage      StatisticsSnapshot `json:"usage"`
}

type StatisticsSnapshot struct {
	TotalRequests  int64                  `json:"total_requests"`
	SuccessCount   int64                  `json:"success_count"`
	FailureCount   int64                  `json:"failure_count"`
	TotalTokens    int64                  `json:"total_tokens"`
	APIs           map[string]APISnapshot `json:"apis"`
	RequestsByDay  map[string]int64       `json:"requests_by_day"`
	RequestsByHour map[string]int64       `json:"requests_by_hour"`
	TokensByDay    map[string]int64       `json:"tokens_by_day"`
	TokensByHour   map[string]int64       `json:"tokens_by_hour"`
}

type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	SuccessCount  int64                    `json:"success_count"`
	FailureCount  int64                    `json:"failure_count"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	SuccessCount  int64           `json:"success_count"`
	FailureCount  int64           `json:"failure_count"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

type RequestDetail struct {
	Timestamp time.Time  `json:"timestamp"`
	LatencyMS int64      `json:"latency_ms"`
	Source    string     `json:"source"`
	AuthIndex string     `json:"auth_index"`
	Failed    bool       `json:"failed"`
	Tokens    TokenStats `json:"tokens"`
}

type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}
