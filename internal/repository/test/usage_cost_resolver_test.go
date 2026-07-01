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

func TestUsageCostResolverPrefersModelAliasAndFallsBackToModel(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-cost-resolver.db")
	upsertUsageCostResolverPrice(t, db, "base-model", 10)
	upsertUsageCostResolverPrice(t, db, "alias-model", 2)

	resolver, err := repository.NewUsageCostResolver(context.Background(), db)
	if err != nil {
		t.Fatalf("NewUsageCostResolver returned error: %v", err)
	}

	aliasResult := resolver.Calculate(repository.UsageCostSubject{
		Model:      "base-model",
		ModelAlias: "alias-model",
		Tokens:     helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})
	assertUsageCostResolverResult(t, aliasResult, 2, true)

	fallbackResult := resolver.Calculate(repository.UsageCostSubject{
		Model:      "base-model",
		ModelAlias: "missing-alias",
		Tokens:     helper.UsageTokenCostInput{InputTokens: 1_000_000},
	})
	assertUsageCostResolverResult(t, fallbackResult, 10, true)
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

func TestListUsageEventsWithFilterUsesModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-events-alias-cost.db")
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
	assertUsageCostClose(t, page.Events[0].CostUSD, 2)
	if !page.Events[0].CostAvailable {
		t.Fatalf("expected alias-priced event cost to be available, got %+v", page.Events[0])
	}
}

func TestBuildUsageOverviewWithFilterUsesHourlyModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-hourly-alias-cost.db")
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
	assertUsageCostClose(t, overview.Summary.TotalCost, 2)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview alias cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterUsesDailyModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-overview-daily-alias-cost.db")
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
	assertUsageCostClose(t, overview.Summary.TotalCost, 2)
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected overview daily alias cost to be available, got %+v", overview.Summary)
	}
}

func TestBuildAnalysisWithFilterUsesModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-alias-cost.db")
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
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 2)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis alias cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 2)
}

func TestBuildAnalysisWithFilterUsesDailyModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-analysis-daily-alias-cost.db")
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
	assertUsageCostClose(t, analysis.CostBreakdown.TotalCostUSD, 2)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected analysis daily alias cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("expected one model efficiency row, got %+v", analysis.ModelEfficiency)
	}
	assertUsageCostClose(t, analysis.ModelEfficiency[0].CostUSD, 2)
}

func TestBuildUsageOverviewRealtimeWithFilterUsesRawModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-alias-cost.db")
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
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 2)
}

func TestBuildUsageOverviewRealtimeWithRecentCacheUsesModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-realtime-cache-alias-cost.db")
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
	assertUsageCostClose(t, *realtime.CurrentUsage.Models[0].CostUSD, 2)
}

func TestSumUsageWindowStatsByAuthIndexUsesRawAndHourlyModelAliasPricing(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-window-alias-cost.db")
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
	assertUsageCostClose(t, rawStats.Cost, 2)

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
	assertUsageCostClose(t, hourlyStats.Cost, 2)
}

func TestSumUsageWindowStatsByAuthIndexMergesRawAndHourlyByModelAliasAndModel(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "usage-window-merged-alias-cost.db")
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
	assertUsageCostClose(t, stats.Cost, 26)
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
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                model,
		PromptPricePer1M:     promptPrice,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
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
