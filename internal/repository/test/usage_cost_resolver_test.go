package test

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func TestUsageCostResolverPrefersModelPricingOverAliasWhenBothPriced(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}

	result := resolver.Calculate(repository.UsageCostSubject{
		Model:      "base-model",
		ModelAlias: "alias-model",
		Tokens:     helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})
	assertUsageCostResolverResult(t, result, 10, true)
	if result.MatchedModel != "base-model" || result.MatchedBy != "model" {
		t.Fatalf("expected resolver to match real model pricing, got %+v", result)
	}
}

func TestUsageCostResolverCostAvailabilityForMissingPrices(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver-missing.db")
	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}

	billable := resolver.Calculate(repository.UsageCostSubject{
		Model:  "missing-model",
		Tokens: helper.UsageTokenCostInput{InputTokens: 1},
	})
	if billable.Available {
		t.Fatalf("expected missing billable price to be unavailable, got %+v", billable)
	}
	if billable.Cost.TotalCostUSD != 0 {
		t.Fatalf("expected missing billable price to cost 0, got %+v", billable.Cost)
	}

	empty := resolver.Calculate(repository.UsageCostSubject{Model: "missing-model"})
	assertUsageCostResolverResult(t, empty, 0, true)
}

func TestUsageCostResolverFallsBackToAliasWhenModelPriceIsMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver-alias-only.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}
	result := resolver.Calculate(repository.UsageCostSubject{
		Model:      "missing-model",
		ModelAlias: "alias-model",
		Tokens:     helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})

	assertUsageCostResolverResult(t, result, 2, true)
	if result.MatchedModel != "alias-model" || result.MatchedBy != "model_alias" {
		t.Fatalf("expected resolver to fall back to alias pricing, got %+v", result)
	}
}

func TestUsageCostResolverTreatsZeroMultiplierAsMatchedAvailableCost(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver-zero-multiplier.db")
	zero := 0.0
	upsertUsageCostResolverPriceWithMultiplier(t, db, "free-model", 10, &zero)

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}
	result := resolver.Calculate(repository.UsageCostSubject{
		Model:  "free-model",
		Tokens: helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})

	assertUsageCostResolverResult(t, result, 0, true)
}

func TestUsageCostResolverDoesNotApplyMultiplierWhenPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver-missing-with-multiplier.db")
	zero := 0.0
	upsertUsageCostResolverPriceWithMultiplier(t, db, "free-model", 10, &zero)

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}
	result := resolver.Calculate(repository.UsageCostSubject{
		Model:  "missing-model",
		Tokens: helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})

	assertUsageCostResolverResult(t, result, 0, false)
}

func TestUsageCostResolverChargesOpenAICacheReadAndWritePrices(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver-openai-cache.db")
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "gpt-5.6-terra",
		PricingStyle:         entities.ModelPricingStyleOpenAI,
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}
	result := resolver.Calculate(repository.UsageCostSubject{
		Model: "gpt-5.6-terra",
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         1_000_000,
			OutputTokens:        500_000,
			CacheReadTokens:     200_000,
			CacheCreationTokens: 100_000,
		},
	})

	if !result.Available || result.PricingStyle != entities.ModelPricingStyleOpenAI {
		t.Fatalf("expected available OpenAI pricing result, got %+v", result)
	}
	assertUsageCostClose(t, result.Cost.UncachedInputCostUSD, 0.7*3)
	assertUsageCostClose(t, result.Cost.CacheReadCostUSD, 0.2*0.3)
	assertUsageCostClose(t, result.Cost.CacheWriteCostUSD, 0.1*3.75)
	assertUsageCostClose(t, result.Cost.OutputCostUSD, 0.5*15)
	assertUsageCostClose(t, result.Cost.TotalCostUSD, 0.7*3+0.5*15+0.2*0.3+0.1*3.75)
}

func TestListUsageEventsWithFilterUsesModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-events-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	eventTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-alias-cost",
		Model:       "base-model",
		ModelAlias:  &alias,
		Timestamp:   eventTime,
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := eventTime.Add(-time.Minute)
	end := eventTime.Add(time.Minute)
	page, err := repository.ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(page.Events))
	}
	assertUsageCostClose(t, page.Events[0].CostUSD, 10)
	if !page.Events[0].CostAvailable {
		t.Fatalf("expected model-priced event cost to be available, got %+v", page.Events[0])
	}
}

func TestListUsageEventsWithFilterFallsBackToAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-events-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	eventTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-alias-fallback-cost",
		Model:       "missing-model",
		ModelAlias:  &alias,
		Timestamp:   eventTime,
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := eventTime.Add(-time.Minute)
	end := eventTime.Add(time.Minute)
	page, err := repository.ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(page.Events))
	}
	if page.Events[0].Model != "missing-model" {
		t.Fatalf("expected usage event to keep real model display value, got %+v", page.Events[0])
	}
	assertUsageCostClose(t, page.Events[0].CostUSD, 2)
	if !page.Events[0].CostAvailable {
		t.Fatalf("expected alias-fallback event cost to be available, got %+v", page.Events[0])
	}
}

func TestBuildUsageOverviewWithFilterUsesHourlyModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-hourly-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket,
		APIGroupKey: "api-key",
		Model:       "base-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   bucket,
		UpdatedAt:   bucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}

	start := bucket
	end := bucket.Add(time.Hour)
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, overview.Summary.TotalCost, 10)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview model cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterFallsBackToHourlyAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-hourly-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket,
		APIGroupKey: "api-key",
		Model:       "missing-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   bucket,
		UpdatedAt:   bucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}

	start := bucket
	end := bucket.Add(time.Hour)
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, overview.Summary.TotalCost, 2)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview hourly alias-fallback cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterUsesDailyModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-daily-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket,
		APIGroupKey: "api-key",
		Model:       "base-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   bucket,
		UpdatedAt:   bucket,
	}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, overview.Summary.TotalCost, 10)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview daily model cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterFallsBackToDailyAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-daily-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket,
		APIGroupKey: "api-key",
		Model:       "missing-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   bucket,
		UpdatedAt:   bucket,
	}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, overview.Summary.TotalCost, 2)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview daily alias-fallback cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildAnalysisWithFilterUsesModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "api-key", DisplayKey: "sk-*********alias"}).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  bucket,
		APIGroupKey:  "api-key",
		Model:        "base-model",
		ModelAlias:   "alias-model",
		RequestCount: 1,
		InputTokens:  1_000_000,
		TotalTokens:  1_000_000,
		CreatedAt:    bucket,
		UpdatedAt:    bucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}

	start := bucket
	end := bucket.Add(time.Hour)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 10)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis model cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 10)
}

func TestBuildAnalysisWithFilterFallsBackToAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "api-key", DisplayKey: "sk-*********alias"}).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  bucket,
		APIGroupKey:  "api-key",
		Model:        "missing-model",
		ModelAlias:   "alias-model",
		RequestCount: 1,
		InputTokens:  1_000_000,
		TotalTokens:  1_000_000,
		CreatedAt:    bucket,
		UpdatedAt:    bucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}

	start := bucket
	end := bucket.Add(time.Hour)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 2)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis alias-fallback cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	if analysis.ModelEfficiency[0].Model != "missing-model" {
		t.Fatalf("expected analysis to keep real model display value, got %+v", analysis.ModelEfficiency[0])
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 2)
}

func TestBuildAnalysisWithFilterUsesDailyModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-daily-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "api-key", DisplayKey: "sk-*********alias"}).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart:  bucket,
		APIGroupKey:  "api-key",
		Model:        "base-model",
		ModelAlias:   "alias-model",
		RequestCount: 1,
		InputTokens:  1_000_000,
		TotalTokens:  1_000_000,
		CreatedAt:    bucket,
		UpdatedAt:    bucket,
	}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 10)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis daily model cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 10)
}

func TestBuildAnalysisWithFilterFallsBackToDailyAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-daily-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	bucket := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "api-key", DisplayKey: "sk-*********alias"}).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart:  bucket,
		APIGroupKey:  "api-key",
		Model:        "missing-model",
		ModelAlias:   "alias-model",
		RequestCount: 1,
		InputTokens:  1_000_000,
		TotalTokens:  1_000_000,
		CreatedAt:    bucket,
		UpdatedAt:    bucket,
	}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 2)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis daily alias-fallback cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	if analysis.ModelEfficiency[0].Model != "missing-model" {
		t.Fatalf("expected analysis daily row to keep real model display value, got %+v", analysis.ModelEfficiency[0])
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 2)
}

func TestBuildUsageOverviewRealtimeWithFilterUsesRawModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "realtime-alias-cost",
		APIGroupKey: "api-key",
		Model:       "base-model",
		ModelAlias:  &alias,
		Timestamp:   now.Add(-time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter returned error: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 {
		t.Fatalf("expected one realtime model row, got %+v", realtime.CurrentUsage.Models)
	}
	if realtime.CurrentUsage.Models[0].CostUSD == nil {
		t.Fatalf("expected realtime model cost to be available, got %+v", realtime.CurrentUsage.Models[0])
	}
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 10)
}

func TestBuildUsageOverviewRealtimeWithFilterFallsBackToRawAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "realtime-alias-fallback-cost",
		APIGroupKey: "api-key",
		Model:       "missing-model",
		ModelAlias:  &alias,
		Timestamp:   now.Add(-time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter returned error: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 {
		t.Fatalf("expected one realtime model row, got %+v", realtime.CurrentUsage.Models)
	}
	if realtime.CurrentUsage.Models[0].Key != "missing-model" {
		t.Fatalf("expected realtime model row to keep real model display value, got %+v", realtime.CurrentUsage.Models[0])
	}
	if realtime.CurrentUsage.Models[0].CostUSD == nil {
		t.Fatalf("expected realtime alias-fallback cost to be available, got %+v", realtime.CurrentUsage.Models[0])
	}
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 2)
}

func TestBuildUsageOverviewRealtimeWithRecentCacheUsesModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-cache-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "realtime-cache-alias-cost",
		APIGroupKey: "api-key",
		Model:       "base-model",
		ModelAlias:  &alias,
		Timestamp:   now.Add(-time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	recentCache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time {
		return now
	}})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(recentCache.Close)

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, recentCache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 {
		t.Fatalf("expected one realtime model row, got %+v", realtime.CurrentUsage.Models)
	}
	if realtime.CurrentUsage.Models[0].CostUSD == nil {
		t.Fatalf("expected realtime cache model cost to be available, got %+v", realtime.CurrentUsage.Models[0])
	}
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 10)
}

func TestBuildUsageOverviewRealtimeWithRecentCacheFallsBackToAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-cache-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "realtime-cache-alias-fallback-cost",
		APIGroupKey: "api-key",
		Model:       "missing-model",
		ModelAlias:  &alias,
		Timestamp:   now.Add(-time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	recentCache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time {
		return now
	}})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(recentCache.Close)

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, recentCache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 {
		t.Fatalf("expected one realtime model row, got %+v", realtime.CurrentUsage.Models)
	}
	if realtime.CurrentUsage.Models[0].Key != "missing-model" {
		t.Fatalf("expected realtime cache model row to keep real model display value, got %+v", realtime.CurrentUsage.Models[0])
	}
	if realtime.CurrentUsage.Models[0].CostUSD == nil {
		t.Fatalf("expected realtime cache alias-fallback cost to be available, got %+v", realtime.CurrentUsage.Models[0])
	}
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 2)
}

func TestSumUsageWindowStatsByAuthIndexUsesRawAndHourlyModelPricingWhenAliasDiffers(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-window-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"

	rawStart := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	rawEnd := rawStart.Add(time.Hour)
	if err := db.Create(&entities.UsageEvent{
		AuthIndex:   "auth-raw",
		Model:       "base-model",
		ModelAlias:  &alias,
		Timestamp:   rawStart.Add(10 * time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed raw usage event: %v", err)
	}
	rawStats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-raw", rawStart, &rawEnd)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex raw returned error: %v", err)
	}
	assertUsageCostClose(t, rawStats.Cost, 10)

	hourlyStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	hourlyEnd := hourlyStart.Add(7 * 24 * time.Hour)
	hourlyBucket := hourlyStart.Add(48 * time.Hour)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: hourlyBucket,
		AuthIndex:   "auth-hourly",
		Model:       "base-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   hourlyBucket,
		UpdatedAt:   hourlyBucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}
	hourlyStats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-hourly", hourlyStart, &hourlyEnd)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex hourly returned error: %v", err)
	}
	assertUsageCostClose(t, hourlyStats.Cost, 10)
}

func TestSumUsageWindowStatsByAuthIndexFallsBackToAliasPricingWhenModelPriceMissing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-window-alias-fallback-cost.db")
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	alias := "alias-model"

	rawStart := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	rawEnd := rawStart.Add(time.Hour)
	if err := db.Create(&entities.UsageEvent{
		AuthIndex:   "auth-raw",
		Model:       "missing-model",
		ModelAlias:  &alias,
		Timestamp:   rawStart.Add(10 * time.Minute),
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed raw usage event: %v", err)
	}
	rawStats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-raw", rawStart, &rawEnd)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex raw returned error: %v", err)
	}
	assertUsageCostClose(t, rawStats.Cost, 2)

	hourlyStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	hourlyEnd := hourlyStart.Add(7 * 24 * time.Hour)
	hourlyBucket := hourlyStart.Add(48 * time.Hour)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: hourlyBucket,
		AuthIndex:   "auth-hourly",
		Model:       "missing-model",
		ModelAlias:  "alias-model",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
		CreatedAt:   hourlyBucket,
		UpdatedAt:   hourlyBucket,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}
	hourlyStats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-hourly", hourlyStart, &hourlyEnd)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex hourly returned error: %v", err)
	}
	assertUsageCostClose(t, hourlyStats.Cost, 2)
}

func TestSumUsageWindowStatsByAuthIndexMergesRawAndHourlyByModelAliasAndModelButPricesByModel(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-window-merged-model-cost.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)
	authIndex := "auth-merged"
	alias := "alias-model"
	start := time.Date(2026, 6, 1, 10, 15, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 18, 45, 0, 0, time.UTC)

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "left-raw-alias", AuthIndex: authIndex, Model: "base-model", ModelAlias: &alias, Timestamp: time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{EventKey: "left-raw-model", AuthIndex: authIndex, Model: "base-model", Timestamp: time.Date(2026, 6, 1, 10, 35, 0, 0, time.UTC), InputTokens: 500_000, TotalTokens: 500_000},
		{EventKey: "right-raw-alias", AuthIndex: authIndex, Model: "base-model", ModelAlias: &alias, Timestamp: time.Date(2026, 6, 1, 17, 30, 0, 0, time.UTC), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{EventKey: "right-raw-model", AuthIndex: authIndex, Model: "base-model", Timestamp: time.Date(2026, 6, 1, 17, 35, 0, 0, time.UTC), InputTokens: 500_000, TotalTokens: 500_000},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := db.Create(&[]entities.UsageOverviewHourlyStat{
		{BucketStart: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), AuthIndex: authIndex, Model: "base-model", ModelAlias: "alias-model", InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{BucketStart: time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC), AuthIndex: authIndex, Model: "base-model", InputTokens: 1_000_000, TotalTokens: 1_000_000},
	}).Error; err != nil {
		t.Fatalf("seed hourly stats: %v", err)
	}

	stats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, authIndex, start, &end)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 5_000_000 {
		t.Fatalf("expected raw and hourly tokens to merge by model_alias/model, got %+v", stats)
	}
	assertUsageCostClose(t, stats.Cost, 50)
}

func openUsageCostResolverDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func upsertUsageCostResolverPrice(t *testing.T, db *gorm.DB, model string, promptPrice float64) {
	t.Helper()
	upsertUsageCostResolverPriceWithMultiplier(t, db, model, promptPrice, nil)
}

func upsertUsageCostResolverPriceWithMultiplier(t *testing.T, db *gorm.DB, model string, promptPrice float64, multiplier *float64) {
	t.Helper()
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                model,
		PromptPricePer1M:     promptPrice,
		CompletionPricePer1M: 0,
		CacheReadPricePer1M:  0,
		PriceMultiplier:      multiplier,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting(%q) returned error: %v", model, err)
	}
}

func assertUsageCostResolverResult(t *testing.T, result repository.UsageCostResult, wantCost float64, wantAvailable bool) {
	t.Helper()
	if result.Available != wantAvailable {
		t.Fatalf("expected available=%v, got %+v", wantAvailable, result)
	}
	assertUsageCostClose(t, result.Cost.TotalCostUSD, wantCost)
}

func assertUsageCostClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000000001 {
		t.Fatalf("expected cost %.8f, got %.8f", want, got)
	}
}
