package test

import (
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestRealtimeInsightsUseVisibleCacheWindowWithoutRawQueries(t *testing.T) {
	for _, window := range []string{"15m", "30m", "60m"} {
		t.Run(window, func(t *testing.T) {
			db := openTestDatabase(t)
			end := time.Now().Truncate(time.Second)
			duration, _ := time.ParseDuration(window)
			start := end.Add(-duration)
			events := []entities.UsageEvent{
				{EventKey: "warmup", Timestamp: start.Add(-time.Second), APIGroupKey: "key-a", Model: "model-a", InputTokens: 9000, TotalTokens: 9000},
				{EventKey: "visible", Timestamp: start, APIGroupKey: "key-a", Model: "model-a", InputTokens: 100, OutputTokens: 20, CacheReadTokens: 40, CacheCreationTokens: 10, ReasoningTokens: 5, TotalTokens: 120},
				{EventKey: "failed", Timestamp: end.Add(-time.Minute), APIGroupKey: "key-a", Model: "model-a", Failed: true, TotalTokens: 999},
				{EventKey: "no-tokens", Timestamp: end.Add(-time.Second), APIGroupKey: "key-a", Model: "model-a"},
				{EventKey: "other-key", Timestamp: end.Add(-time.Second), APIGroupKey: "key-b", Model: "model-b", InputTokens: 10000, TotalTokens: 10000},
				{EventKey: "excluded-end", Timestamp: end, APIGroupKey: "key-a", Model: "model-a", TotalTokens: 8000},
			}
			if err := db.Create(&events).Error; err != nil {
				t.Fatal(err)
			}
			cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cache.Close)
			queries := captureOverviewDataQueries(t, db, "insights_"+window)
			result, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{RealtimeWindow: window, RealtimeEndTime: &end, APIGroupKey: "key-a"}, cache, repositoryPricingResolver(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			s := result.Insights.Summary
			if s.Requests != 3 || s.Failures != 1 || s.TokenRequests != 1 || s.CachedRequests != 1 || s.TotalTokens != 120 || s.InputTokens != 100 || s.OutputTokens != 20 || s.ReasoningTokens != 5 || s.CacheReadTokens != 40 || s.CacheCreationTokens != 10 {
				t.Fatalf("window totals included warmup, other key, failure tokens or end: %+v", s)
			}
			if !s.CostAvailable || math.Abs(s.CostUSD-0.00005) > 1e-9 {
				t.Fatalf("window cost: %+v", s)
			}
			if len(result.Insights.Outcomes) != 30 {
				t.Fatal("outcomes must stay bounded to 30 points")
			}
			var requests, failures int64
			for _, point := range result.Insights.Outcomes {
				requests += point.Requests
				failures += point.Failures
			}
			if requests != s.Requests || failures != s.Failures {
				t.Fatal("outcomes must be non-overlapping window counts")
			}
			if strings.Contains(strings.Join(*queries, "\n"), "usage_events") {
				t.Fatal("cache-backed insights queried raw events")
			}
		})
	}
}

func TestRealtimeInsightsKeepMissingCostAndEmptyWindowDistinct(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Now().Truncate(time.Second)
	if err := db.Create(&entities.UsageEvent{EventKey: "missing-price", Timestamp: end.Add(-time.Minute), Model: "unpriced", TotalTokens: 100, InputTokens: 100}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Insights.Summary.CostAvailable || result.Insights.Summary.TotalTokens != 100 {
		t.Fatal("missing price must not become a known zero cost")
	}
	result, err = repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end, APIGroupKey: "unused"}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Insights.Summary.CostAvailable || result.Insights.Summary.Requests != 0 || len(result.Insights.Outcomes) != 30 {
		t.Fatal("empty window should retain an explicit zero summary and bounded timeline")
	}
}
