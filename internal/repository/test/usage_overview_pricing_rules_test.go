package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageOverviewHourlyAppliesActiveRuleDimensionsBeforePricing(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "default", InputTokens: 1_000_000, TotalTokens: 1_000_000},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed hourly pricing rows: %v", err)
	}
	end := bucket.Add(time.Hour)
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter: %v", err)
	}
	if overview.Summary.TotalCost != 3 || !overview.Summary.CostAvailable {
		t.Fatalf("expected default cost 1 + priority cost 2, got %+v", overview.Summary)
	}
}

func TestUsageOverviewDailyAppliesTwoMatchingRulesContinuously(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Now().AddDate(0, 0, -2).Truncate(24 * time.Hour)
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed daily pricing row: %v", err)
	}
	end := bucket.Add(24 * time.Hour)
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	})
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "day", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, resolver)
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
