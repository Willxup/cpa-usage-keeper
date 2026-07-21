package test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

func TestUsageActivityMapsNormalizedTimeRangesToFixedWindowGrains(t *testing.T) {
	db := openUsageActivityServiceDatabase(t)
	provider := service.NewUsageService(db)
	now := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	testCases := []struct {
		name         string
		filter       servicedto.UsageFilter
		wantWindow   servicedto.UsageActivityWindow
		wantGrain    string
		wantDuration time.Duration
	}{
		{name: "hours", filter: servicedto.UsageFilter{Range: "8h", RangeUnit: "hour", RangeCount: 8}, wantWindow: servicedto.UsageActivityWindow24H, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "one day", filter: servicedto.UsageFilter{Range: "today", RangeUnit: "day", RangeCount: 1}, wantWindow: servicedto.UsageActivityWindow24H, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "two days", filter: servicedto.UsageFilter{Range: "2d", RangeUnit: "day", RangeCount: 2}, wantWindow: servicedto.UsageActivityWindow7D, wantGrain: "medium", wantDuration: 7 * 24 * time.Hour},
		{name: "seven days", filter: servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 7}, wantWindow: servicedto.UsageActivityWindow7D, wantGrain: "medium", wantDuration: 7 * 24 * time.Hour},
		{name: "eight days", filter: servicedto.UsageFilter{Range: "8d", RangeUnit: "day", RangeCount: 8}, wantWindow: servicedto.UsageActivityWindow30D, wantGrain: "long", wantDuration: 30 * 24 * time.Hour},
		{name: "one year", filter: servicedto.UsageFilter{ActivityWindow: servicedto.UsageActivityWindow1Y}, wantWindow: servicedto.UsageActivityWindow1Y, wantGrain: "daily", wantDuration: 364 * 24 * time.Hour},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			filter := testCase.filter
			filter.QueryNow = &now
			activity, err := provider.GetUsageActivity(context.Background(), filter)
			if err != nil {
				t.Fatalf("GetUsageActivity returned error: %v", err)
			}
			if activity.Window != testCase.wantWindow || activity.Grain != testCase.wantGrain {
				t.Fatalf("unexpected Activity identity: window=%q grain=%q", activity.Window, activity.Grain)
			}
			if activity.Rows != 7 || activity.Columns != 52 || len(activity.Blocks) != repository.UsageActivityHeatmapBlocks {
				t.Fatalf("unexpected Activity shape: rows=%d columns=%d blocks=%d", activity.Rows, activity.Columns, len(activity.Blocks))
			}
			if got := activity.WindowEnd.Sub(activity.WindowStart); got != testCase.wantDuration {
				t.Fatalf("Activity duration=%s, want %s", got, testCase.wantDuration)
			}
			for index, block := range activity.Blocks {
				if block.Rate != -1 {
					t.Fatalf("empty block %d rate=%v, want -1", index, block.Rate)
				}
			}
		})
	}
}

func TestUsageActivityHeaderAndBlocksUseTheSameAPIKeyScope(t *testing.T) {
	db := openUsageActivityServiceDatabase(t)
	apiKey := entities.CPAAPIKey{APIKey: "provider-a", DisplayKey: "provider-a"}
	if err := db.Create(&apiKey).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	bucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainShort, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("resolve Activity bucket: %v", err)
	}
	rows := []entities.UsageActivityStat{
		{Grain: entities.UsageActivityGrainShort, BucketStart: bucket.Start, BucketEnd: bucket.End, APIGroupKey: "provider-a", SuccessCount: 1, FailureCount: 1, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 30, CacheReadTokens: 40, CacheCreationTokens: 50, TotalTokens: 777},
		{Grain: entities.UsageActivityGrainShort, BucketStart: bucket.Start, BucketEnd: bucket.End, APIGroupKey: "provider-b", SuccessCount: 9, InputTokens: 900, TotalTokens: 999},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed Activity rows: %v", err)
	}

	activity, err := service.NewUsageService(db).GetUsageActivity(context.Background(), servicedto.UsageFilter{
		Range: "24h", RangeUnit: "hour", RangeCount: 24, QueryNow: &now, APIKeyID: fmt.Sprint(apiKey.ID),
	})
	if err != nil {
		t.Fatalf("GetUsageActivity returned error: %v", err)
	}
	if activity.TotalSuccess != 1 || activity.TotalFailure != 1 || activity.SuccessRate != 50 {
		t.Fatalf("unexpected scoped Activity header: success=%d failure=%d rate=%v", activity.TotalSuccess, activity.TotalFailure, activity.SuccessRate)
	}
	if activity.InputTokens != 10 || activity.OutputTokens != 20 || activity.ReasoningTokens != 30 || activity.CacheReadTokens != 40 || activity.CacheCreationTokens != 50 || activity.TotalTokens != 777 {
		t.Fatalf("unexpected scoped Activity Token totals: %+v", activity)
	}
	var blockSuccess, blockFailure int64
	var blockInputTokens, blockTotalTokens int64
	for _, block := range activity.Blocks {
		blockSuccess += block.Success
		blockFailure += block.Failure
		blockInputTokens += block.InputTokens
		blockTotalTokens += block.TotalTokens
	}
	if blockSuccess != activity.TotalSuccess || blockFailure != activity.TotalFailure {
		t.Fatalf("Activity header and blocks disagree: header=%d/%d blocks=%d/%d", activity.TotalSuccess, activity.TotalFailure, blockSuccess, blockFailure)
	}
	if blockInputTokens != activity.InputTokens || blockTotalTokens != activity.TotalTokens {
		t.Fatalf("Activity Token header and blocks disagree: header=%d/%d blocks=%d/%d", activity.InputTokens, activity.TotalTokens, blockInputTokens, blockTotalTokens)
	}
}

func openUsageActivityServiceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-activity-service.db")})
	if err != nil {
		t.Fatalf("open Activity service database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("resolve Activity service database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
