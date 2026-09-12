package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// 所有新指标复用同一次缓存读取和事件分桶；这里只遍历 30 个可见桶，不对重叠的滚动点求和。
func buildRealtimeInsights(buckets []usageOverviewRealtimeBucket) dto.RealtimeInsightsRecord {
	result := dto.RealtimeInsightsRecord{
		Summary:  dto.RealtimeWindowSummaryRecord{CostAvailable: true},
		Outcomes: make([]dto.RealtimeOutcomePointRecord, 0, len(buckets)),
	}
	for _, bucket := range buckets {
		s := &result.Summary
		s.Requests += bucket.requests
		s.Failures += bucket.failures
		s.TokenRequests += bucket.tokenRequests
		s.CachedRequests += bucket.cachedRequests
		s.TotalTokens += bucket.tokens
		s.InputTokens += bucket.inputTokens
		s.OutputTokens += bucket.outputTokens
		s.ReasoningTokens += bucket.reasoningTokens
		s.CacheReadTokens += bucket.cacheReadTokens
		s.CacheCreationTokens += bucket.cacheCreationTokens
		s.CostUSD += bucket.costUSD
		s.CostAvailable = s.CostAvailable && bucket.costAvailable
		result.Outcomes = append(result.Outcomes, dto.RealtimeOutcomePointRecord{
			Bucket: timeutil.FormatStorageTime(bucket.bucketStart), Requests: bucket.requests, Failures: bucket.failures,
		})
	}
	return result
}
