package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageAnalysisPreservesStoredCostWithPricingDimensions(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	cost, unavailable := 6.0, int64(0)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", RequestCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000,
		CostUSD: &cost, UnavailableCostCount: &unavailable,
	}).Error; err != nil {
		t.Fatalf("seed analysis pricing row: %v", err)
	}
	end := bucket.Add(time.Hour)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &bucket, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}
	if analysis.CostSummary.TotalCostUSD != 6 || !analysis.CostSummary.CostAvailable {
		t.Fatalf("expected analysis cost 6, got %+v", analysis.CostSummary)
	}
}
