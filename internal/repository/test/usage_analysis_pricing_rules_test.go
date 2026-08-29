package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageAnalysisMapsCompleteRollupRowIntoPricingResolver(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", RequestCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed analysis pricing row: %v", err)
	}
	end := bucket.Add(time.Hour)
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	})
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &bucket, EndTime: &end, EndExclusive: true}, resolver)
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}
	if analysis.CostBreakdown.TotalCostUSD != 6 || !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis cost 6, got %+v", analysis.CostBreakdown)
	}
}
