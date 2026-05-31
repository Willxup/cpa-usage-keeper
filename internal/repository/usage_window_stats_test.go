package repository

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestSumUsageWindowStatsByAuthIndexUsesAuthIndexAndWindow(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "priced", PromptPricePer1M: 10, CompletionPricePer1M: 20, CachePricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	events := []entities.UsageEvent{
		{AuthType: "oauth", AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000, TotalTokens: 1_500_000},
		{AuthType: "apikey", AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(15 * time.Minute), InputTokens: 700_000, TotalTokens: 700_000},
		{AuthType: "oauth", AuthIndex: "auth-2", Model: "priced", Timestamp: start.Add(20 * time.Minute), TotalTokens: 9_000_000},
		{AuthType: "oauth", AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(time.Minute), TotalTokens: 8_000_000},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	stats, err := SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-1", start, &end)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 2_200_000 {
		t.Fatalf("expected 2200000 tokens, got %d", stats.Tokens)
	}
	wantCost := 1.5*10 + 0.5*20 + 0.2*1
	if stats.Cost != wantCost {
		t.Fatalf("expected cost %.2f, got %.2f", wantCost, stats.Cost)
	}
}

func TestSumUsageWindowStatsByAuthIndexCalculatesClaudeCacheReadAndCreationCost(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats-claude.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
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
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	stats, err := SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-claude", start, &end)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex returned error: %v", err)
	}
	wantCost := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	if math.Abs(stats.Cost-wantCost) > 0.000000001 {
		t.Fatalf("expected Claude cache read/write cost %.8f, got %.8f", wantCost, stats.Cost)
	}
}

func TestSumUsageWindowStatsByAuthIndexUsesHourlyStatsForLongWindow(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats-hourly.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "priced", PromptPricePer1M: 10, CompletionPricePer1M: 20, CachePricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	start := time.Date(2026, 5, 18, 14, 30, 0, 0, time.UTC)
	end := time.Date(2026, 5, 25, 19, 20, 0, 0, time.UTC)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(-50 * time.Minute), InputTokens: 400_000, TotalTokens: 400_000},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: end.Add(-10 * time.Minute), OutputTokens: 500_000, TotalTokens: 500_000},
		{AuthIndex: "auth-1", Model: "priced", Timestamp: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), InputTokens: 9_000_000, TotalTokens: 9_000_000},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}
	hourly := entities.UsageOverviewHourlyStat{BucketStart: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC), AuthIndex: "auth-1", Model: "priced", InputTokens: 2_000_000, CachedTokens: 300_000, TotalTokens: 2_000_000, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&hourly).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewAggregationCheckpoint{Name: usageOverviewAggregationCheckpointName, LastAggregatedUsageEventID: 4, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}
	if err := db.Where("total_tokens = ?", int64(9_000_000)).Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("delete full-hour raw events: %v", err)
	}

	stats, err := SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-1", start, &end)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 3_900_000 {
		t.Fatalf("expected hourly plus boundary tokens, got %d", stats.Tokens)
	}
	wantCost := 3.1*10 + 0.5*20 + 0.3*1
	if stats.Cost != wantCost {
		t.Fatalf("expected cost %.2f, got %.2f", wantCost, stats.Cost)
	}
}

func TestSumLongUsageWindowTokenStatsDoesNotDoubleCountWhenBoundaryClips(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats-overlap.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 5, 25, 14, 30, 0, 0, time.UTC)
	end := time.Date(2026, 5, 25, 15, 20, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageEvent{AuthIndex: "auth-1", Model: "priced", Timestamp: start.Add(10 * time.Minute), TotalTokens: 1_000_000}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	rows, err := sumLongUsageWindowTokenStats(db, "auth-1", start, end)
	if err != nil {
		t.Fatalf("sumLongUsageWindowTokenStats returned error: %v", err)
	}
	stats := usageWindowStatsFromTokenStats(rows, nil)
	if stats.Tokens != 1_000_000 {
		t.Fatalf("expected clipped boundaries to count event once, got %d", stats.Tokens)
	}
}

func TestSumUsageWindowStatsByAuthIndexIgnoresZeroWindowTimes(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats-zero-time.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	zero := time.Time{}
	if err := db.Create(&entities.UsageEvent{AuthIndex: "auth-1", Model: "priced", Timestamp: start, TotalTokens: 1_000_000}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	stats, err := SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-1", time.Time{}, nil)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex with zero start returned error: %v", err)
	}
	if stats.Tokens != 0 || stats.Cost != 0 {
		t.Fatalf("expected zero start to return empty stats, got %+v", stats)
	}
	stats, err = SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-1", start, &zero)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex with zero end returned error: %v", err)
	}
	if stats.Tokens != 0 || stats.Cost != 0 {
		t.Fatalf("expected zero end to return empty stats, got %+v", stats)
	}
}

func TestSumUsageWindowStatsByAuthIndexTreatsMissingPriceAsZeroCost(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-window-stats-missing-price.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageEvent{AuthType: "oauth", AuthIndex: "auth-1", Model: "missing", Timestamp: start, InputTokens: 1_000_000, TotalTokens: 1_000_000}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	stats, err := SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-1", start.Add(-time.Minute), nil)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex returned error: %v", err)
	}
	if stats.Tokens != 1_000_000 || stats.Cost != 0 {
		t.Fatalf("expected tokens with zero missing-price cost, got %+v", stats)
	}
}
