package test

import (
	"context"
	. "cpa-usage-keeper/internal/repository"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func newUsageWindowCalculatorForTest(t *testing.T, db *gorm.DB) *UsageWindowStatsCalculator {
	t.Helper()
	calculator, err := NewUsageWindowStatsCalculator(db)
	if err != nil {
		t.Fatalf("create window calculator: %v", err)
	}
	return calculator
}

func windowCostPtr(value float64) *float64 { return &value }

func windowAvailablePtr(value bool) *bool { return &value }

func windowUnavailablePtr(value int64) *int64 { return &value }

func TestUsageWindowUsesAuthIndexAndHalfOpenRange(t *testing.T) {
	db := openTestDatabase(t)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "priced", PromptPricePer1M: 10, CompletionPricePer1M: 20, CacheReadPricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	events := []entities.UsageEvent{
		{AuthType: "oauth", AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000, CacheReadTokens: 200_000, TotalTokens: 1_500_000, CostUSD: windowCostPtr(18.2), CostAvailable: windowAvailablePtr(true)},
		{AuthType: "apikey", AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(15 * time.Minute), InputTokens: 700_000, TotalTokens: 700_000, CostUSD: windowCostPtr(7), CostAvailable: windowAvailablePtr(true)},
		{AuthType: "oauth", AuthIndex: "auth-2", Model: "priced", Timestamp: start.Add(20 * time.Minute), TotalTokens: 9_000_000, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false)},
		{AuthType: "oauth", AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(time.Minute), TotalTokens: 8_000_000, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-1", start, &end)
	if err != nil {
		t.Fatalf("SumByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 2_200_000 {
		t.Fatalf("expected 2200000 tokens, got %d", stats.Tokens)
	}
	wantCost := 1.5*10 + 0.5*20 + 0.2*1
	if math.Abs(stats.Cost-wantCost) > 1e-9 {
		t.Fatalf("expected cost %.2f, got %.2f", wantCost, stats.Cost)
	}
}

func TestUsageWindowReadsStoredClaudeTotalCost(t *testing.T) {
	db := openTestDatabase(t)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PricingStyle:         entities.ModelPricingStyleClaude,
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CacheReadPricePer1M:  1,
		CacheWritePricePer1M: 12.5,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	if err := db.Create(&entities.UsageEvent{
		AuthIndex:           "auth-claude",
		Model:               "claude-sonnet",
		Timestamp:           start.Add(10 * time.Minute),
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CachedTokens:        200_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
		TotalTokens:         1_800_000,
		CostUSD:             windowCostPtr(21.45),
		CostAvailable:       windowAvailablePtr(true),
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-claude", start, &end)
	if err != nil {
		t.Fatalf("SumByAuthIndex returned error: %v", err)
	}
	wantCost := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	if math.Abs(stats.Cost-wantCost) > 0.000000001 {
		t.Fatalf("expected Claude cache read/write cost %.8f, got %.8f", wantCost, stats.Cost)
	}
}

func TestUsageWindowUsesHourlyStatsForLongRange(t *testing.T) {
	db := openTestDatabase(t)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "priced", PromptPricePer1M: 10, CompletionPricePer1M: 20, CacheReadPricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	start := time.Date(2026, 5, 18, 14, 30, 0, 0, time.UTC)
	end := time.Date(2026, 5, 25, 19, 20, 0, 0, time.UTC)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(10), CostAvailable: windowAvailablePtr(true)},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(-50 * time.Minute), InputTokens: 400_000, TotalTokens: 400_000, CostUSD: windowCostPtr(4), CostAvailable: windowAvailablePtr(true)},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(-10 * time.Minute), OutputTokens: 500_000, TotalTokens: 500_000, CostUSD: windowCostPtr(10), CostAvailable: windowAvailablePtr(true)},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), InputTokens: 9_000_000, TotalTokens: 9_000_000, CostUSD: windowCostPtr(90), CostAvailable: windowAvailablePtr(true)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}
	hourly := entities.UsageOverviewHourlyStat{BucketStart: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), AuthIndex: "auth-1", Model: "priced", InputTokens: 2_000_000, CachedTokens: 300_000, CacheReadTokens: 300_000, TotalTokens: 2_000_000, CostUSD: windowCostPtr(17.3), UnavailableCostCount: windowUnavailablePtr(0), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&hourly).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 4, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}
	if err := db.Where("total_tokens = ?", int64(9_000_000)).Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("delete full-hour raw events: %v", err)
	}

	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-1", start, &end)
	if err != nil {
		t.Fatalf("SumByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 3_900_000 {
		t.Fatalf("expected hourly plus boundary tokens, got %d", stats.Tokens)
	}
	wantCost := 3.1*10 + 0.5*20 + 0.3*1
	if math.Abs(stats.Cost-wantCost) > 1e-9 {
		t.Fatalf("expected cost %.2f, got %.2f", wantCost, stats.Cost)
	}
}

func TestLongUsageWindowStoredStatsDoesNotDoubleCountWhenBoundaryClips(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 5, 25, 14, 30, 0, 0, time.UTC)
	end := time.Date(2026, 5, 25, 15, 20, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageEvent{AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), TotalTokens: 1_000_000, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false)}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	rows, err := sumLongUsageWindowStoredStats(db, "auth-1", start, end)
	if err != nil {
		t.Fatalf("sumLongUsageWindowStoredStats returned error: %v", err)
	}
	stats, err := usageWindowStatsFromStoredRows(rows)
	if err != nil {
		t.Fatalf("read clipped usage window stats: %v", err)
	}
	if stats.Tokens != 1_000_000 {
		t.Fatalf("expected clipped boundaries to count event once, got %d", stats.Tokens)
	}
}

func TestUsageWindowIgnoresZeroWindowTimes(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	zero := time.Time{}
	if err := db.Create(&entities.UsageEvent{AuthIndex: "auth-1", Model: "priced", Timestamp: start, TotalTokens: 1_000_000, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false)}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	for _, window := range []struct {
		name  string
		start time.Time
		end   *time.Time
	}{
		{name: "zero start"},
		{name: "zero end", start: start, end: &zero},
	} {
		t.Run(window.name, func(t *testing.T) {
			stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-1", window.start, window.end)
			if err != nil {
				t.Fatalf("SumByAuthIndex: %v", err)
			}
			if stats.Tokens != 0 || stats.Cost != 0 {
				t.Fatalf("expected empty stats, got %+v", stats)
			}
		})
	}
}

func TestUsageWindowReadsStoredUnavailableZeroCost(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageEvent{AuthType: "oauth", AuthIndex: "auth-1", Model: "missing", Timestamp: start, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false)}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-1", start.Add(-time.Minute), nil)
	if err != nil {
		t.Fatalf("SumByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 1_000_000 || stats.Cost != 0 || stats.CostAvailable {
		t.Fatalf("expected tokens with zero missing-price cost, got %+v", stats)
	}
}
