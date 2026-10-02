package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageOverviewHourlySumsStoredCostsAcrossRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	priorityCost, defaultCost, unavailable := 2.0, 1.0, int64(0)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &priorityCost, UnavailableCostCount: &unavailable},
		{BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "default", InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &defaultCost, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed hourly pricing rows: %v", err)
	}
	end := bucket.Add(time.Hour)
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter: %v", err)
	}
	if overview.Summary.TotalCost != 3 || !overview.Summary.CostAvailable {
		t.Fatalf("expected default cost 1 + priority cost 2, got %+v", overview.Summary)
	}
}

func TestUsageOverviewDailyReadsStoredCostAcrossFormerRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	cost, unavailable := 6.0, int64(0)
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", InputTokens: 1_000_000, TotalTokens: 1_000_000,
		CostUSD: &cost, UnavailableCostCount: &unavailable,
	}).Error; err != nil {
		t.Fatalf("seed daily pricing row: %v", err)
	}
	end := bucket.Add(24 * time.Hour)
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "day", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter: %v", err)
	}
	if overview.Summary.TotalCost != 6 || !overview.Summary.CostAvailable {
		t.Fatalf("expected continuous daily rule cost 6, got %+v", overview.Summary)
	}
}

func repositoryPricingResolver(t *testing.T, rules []pricing.RuleConfig) pricing.Resolver {
	t.Helper()
	multiplier := 1.0
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{
		Pricing: entities.ModelPriceSetting{Model: "model-a", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1, PriceMultiplier: &multiplier},
		Rules:   rules,
	}})
	if err != nil {
		t.Fatalf("CompileSnapshot: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}
