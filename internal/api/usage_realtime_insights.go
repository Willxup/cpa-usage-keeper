package api

import repodto "cpa-usage-keeper/internal/repository/dto"

type usageRealtimeWindowSummary struct {
	Requests            int64    `json:"requests"`
	Failures            int64    `json:"failures"`
	TokenRequests       int64    `json:"token_requests"`
	CachedRequests      int64    `json:"cached_requests"`
	TotalTokens         int64    `json:"total_tokens"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	Cost                *float64 `json:"cost"`
}

type usageRealtimeOutcomePoint struct {
	Bucket   string `json:"bucket"`
	Requests int64  `json:"requests"`
	Failures int64  `json:"failures"`
}

type usageRealtimeInsights struct {
	Summary  usageRealtimeWindowSummary  `json:"summary"`
	Outcomes []usageRealtimeOutcomePoint `json:"outcomes"`
}

func mapUsageRealtimeInsights(insights *repodto.RealtimeInsightsRecord) *usageRealtimeInsights {
	if insights == nil {
		return nil
	}
	s := insights.Summary
	result := &usageRealtimeInsights{
		Summary: usageRealtimeWindowSummary{
			Requests: s.Requests, Failures: s.Failures, TokenRequests: s.TokenRequests, CachedRequests: s.CachedRequests,
			TotalTokens: s.TotalTokens, InputTokens: s.InputTokens, OutputTokens: s.OutputTokens, ReasoningTokens: s.ReasoningTokens,
			CacheReadTokens: s.CacheReadTokens, CacheCreationTokens: s.CacheCreationTokens,
		},
		Outcomes: make([]usageRealtimeOutcomePoint, 0, len(insights.Outcomes)),
	}
	if s.CostAvailable {
		result.Summary.Cost = &s.CostUSD
	}
	for _, point := range insights.Outcomes {
		result.Outcomes = append(result.Outcomes, usageRealtimeOutcomePoint{Bucket: point.Bucket, Requests: point.Requests, Failures: point.Failures})
	}
	return result
}
