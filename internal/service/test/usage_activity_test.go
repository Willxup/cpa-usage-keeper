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
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	now := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	testCases := []struct {
		name         string
		filter       servicedto.UsageFilter
		wantWindow   servicedto.UsageActivityWindow
		wantGrain    string
		wantDuration time.Duration
		wantDays     int
	}{
		{name: "hours", filter: servicedto.UsageFilter{Range: "8h", RangeUnit: "hour", RangeCount: 8}, wantWindow: servicedto.UsageActivityWindowDay, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "custom hours", filter: servicedto.UsageFilter{Range: "custom", CustomUnit: "hour", RangeUnit: "hour", RangeCount: 8}, wantWindow: servicedto.UsageActivityWindowDay, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "one day", filter: servicedto.UsageFilter{Range: "today", RangeUnit: "day", RangeCount: 1}, wantWindow: servicedto.UsageActivityWindowDay, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "explicit day", filter: servicedto.UsageFilter{ActivityWindow: servicedto.UsageActivityWindowDay}, wantWindow: servicedto.UsageActivityWindowDay, wantGrain: "short", wantDuration: 24 * time.Hour},
		{name: "one custom day", filter: servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 1}, wantWindow: servicedto.UsageActivityWindowYear, wantGrain: "daily", wantDays: repository.UsageActivityHeatmapBlocks},
		{name: "two days", filter: servicedto.UsageFilter{Range: "2d", RangeUnit: "day", RangeCount: 2}, wantWindow: servicedto.UsageActivityWindowWeek, wantGrain: "medium", wantDuration: 7 * 24 * time.Hour},
		{name: "explicit week", filter: servicedto.UsageFilter{ActivityWindow: servicedto.UsageActivityWindowWeek}, wantWindow: servicedto.UsageActivityWindowWeek, wantGrain: "medium", wantDuration: 7 * 24 * time.Hour},
		{name: "seven custom days", filter: servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 7}, wantWindow: servicedto.UsageActivityWindowYear, wantGrain: "daily", wantDays: repository.UsageActivityHeatmapBlocks},
		{name: "eight days", filter: servicedto.UsageFilter{Range: "8d", RangeUnit: "day", RangeCount: 8}, wantWindow: servicedto.UsageActivityWindowMonth, wantGrain: "long", wantDuration: 30 * 24 * time.Hour},
		{name: "explicit month", filter: servicedto.UsageFilter{ActivityWindow: servicedto.UsageActivityWindowMonth}, wantWindow: servicedto.UsageActivityWindowMonth, wantGrain: "long", wantDuration: 30 * 24 * time.Hour},
		{name: "long custom days", filter: servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 121}, wantWindow: servicedto.UsageActivityWindowYear, wantGrain: "daily", wantDays: repository.UsageActivityHeatmapBlocks},
		{name: "explicit year", filter: servicedto.UsageFilter{ActivityWindow: servicedto.UsageActivityWindowYear}, wantWindow: servicedto.UsageActivityWindowYear, wantGrain: "daily", wantDays: repository.UsageActivityHeatmapBlocks},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			filter := testCase.filter
			filter.QueryNow = &now
			if filter.Range == "today" || filter.Range == "yesterday" {
				start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
				if filter.Range == "yesterday" {
					start = start.AddDate(0, 0, -1)
				}
				end := start.AddDate(0, 0, 1).Add(-time.Nanosecond)
				filter.StartTime = &start
				filter.EndTime = &end
			}
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
			if testCase.wantDays > 0 {
				if wantEnd := activity.WindowStart.AddDate(0, 0, testCase.wantDays); !activity.WindowEnd.Equal(wantEnd) {
					t.Fatalf("Activity calendar end=%s, want %s", activity.WindowEnd, wantEnd)
				}
			} else if got := activity.WindowEnd.Sub(activity.WindowStart); got != testCase.wantDuration {
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

func TestUsageActivityDirectWindowsMatchEquivalentNormalizedRanges(t *testing.T) {
	provider := service.NewUsageService(openUsageActivityServiceDatabase(t), emptyPricingCatalogForTest())
	now := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	testCases := []struct {
		name       string
		window     servicedto.UsageActivityWindow
		rangeInput servicedto.UsageFilter
	}{
		{name: "day from hours", window: servicedto.UsageActivityWindowDay, rangeInput: servicedto.UsageFilter{Range: "8h", RangeUnit: "hour", RangeCount: 8}},
		{name: "day from one day", window: servicedto.UsageActivityWindowDay, rangeInput: servicedto.UsageFilter{Range: "1d", RangeUnit: "day", RangeCount: 1}},
		{name: "week lower boundary", window: servicedto.UsageActivityWindowWeek, rangeInput: servicedto.UsageFilter{Range: "2d", RangeUnit: "day", RangeCount: 2}},
		{name: "week upper boundary", window: servicedto.UsageActivityWindowWeek, rangeInput: servicedto.UsageFilter{Range: "7d", RangeUnit: "day", RangeCount: 7}},
		{name: "month lower boundary", window: servicedto.UsageActivityWindowMonth, rangeInput: servicedto.UsageFilter{Range: "8d", RangeUnit: "day", RangeCount: 8}},
		{name: "month upper boundary", window: servicedto.UsageActivityWindowMonth, rangeInput: servicedto.UsageFilter{Range: "30d", RangeUnit: "day", RangeCount: 30}},
		{name: "year from Custom day", window: servicedto.UsageActivityWindowYear, rangeInput: servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 7}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			direct, err := provider.GetUsageActivity(context.Background(), servicedto.UsageFilter{
				ActivityWindow: testCase.window,
				QueryNow:       &now,
			})
			if err != nil {
				t.Fatalf("GetUsageActivity direct window returned error: %v", err)
			}
			rangeInput := testCase.rangeInput
			rangeInput.QueryNow = &now
			normalized, err := provider.GetUsageActivity(context.Background(), rangeInput)
			if err != nil {
				t.Fatalf("GetUsageActivity normalized range returned error: %v", err)
			}

			if direct.Window != normalized.Window || direct.Grain != normalized.Grain || direct.BucketSeconds != normalized.BucketSeconds {
				t.Fatalf("Activity identity differs: direct=%q/%q/%d normalized=%q/%q/%d", direct.Window, direct.Grain, direct.BucketSeconds, normalized.Window, normalized.Grain, normalized.BucketSeconds)
			}
			if !direct.WindowStart.Equal(normalized.WindowStart) || !direct.WindowEnd.Equal(normalized.WindowEnd) {
				t.Fatalf("Activity boundaries differ: direct=%s..%s normalized=%s..%s", direct.WindowStart, direct.WindowEnd, normalized.WindowStart, normalized.WindowEnd)
			}
			if direct.Rows != normalized.Rows || direct.Columns != normalized.Columns || len(direct.Blocks) != len(normalized.Blocks) {
				t.Fatalf("Activity shape differs: direct=%dx%d/%d normalized=%dx%d/%d", direct.Rows, direct.Columns, len(direct.Blocks), normalized.Rows, normalized.Columns, len(normalized.Blocks))
			}
			for index := range direct.Blocks {
				if !direct.Blocks[index].StartTime.Equal(normalized.Blocks[index].StartTime) || !direct.Blocks[index].EndTime.Equal(normalized.Blocks[index].EndTime) {
					t.Fatalf("Activity block %d boundaries differ: direct=%s..%s normalized=%s..%s", index, direct.Blocks[index].StartTime, direct.Blocks[index].EndTime, normalized.Blocks[index].StartTime, normalized.Blocks[index].EndTime)
				}
			}
		})
	}
}

func TestCustomDayUsageActivityReadsDailyRollupWithoutRawEvents(t *testing.T) {
	db := openUsageActivityServiceDatabase(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	bucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainDaily, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("resolve daily Activity bucket: %v", err)
	}
	row := entities.UsageActivityStat{
		Grain: entities.UsageActivityGrainDaily, BucketStart: bucket.Start, BucketEnd: bucket.End,
		APIGroupKey: "provider-a", SuccessCount: 2, FailureCount: 1, InputTokens: 10, TotalTokens: 30,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed daily Activity row: %v", err)
	}
	if err := db.Migrator().DropTable("usage_events"); err != nil {
		t.Fatalf("drop raw usage events: %v", err)
	}

	activity, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetUsageActivity(context.Background(), servicedto.UsageFilter{
		Range: "custom", CustomUnit: "day", RangeUnit: "day", RangeCount: 7, QueryNow: &now,
	})
	if err != nil {
		t.Fatalf("GetUsageActivity without raw events returned error: %v", err)
	}
	if activity.Window != servicedto.UsageActivityWindowYear || activity.Grain != string(entities.UsageActivityGrainDaily) {
		t.Fatalf("unexpected Custom day Activity identity: window=%q grain=%q", activity.Window, activity.Grain)
	}
	if activity.TotalSuccess != 2 || activity.TotalFailure != 1 || activity.InputTokens != 10 || activity.TotalTokens != 30 {
		t.Fatalf("Custom day Activity did not use the daily rollup: %+v", activity)
	}
}

func TestUsageActivityNaturalDayUsesExactLocalDayAndExcludesAdjacentEvents(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	db := openUsageActivityServiceDatabase(t)
	dayStart := time.Date(2026, 7, 20, 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	events := []entities.UsageEvent{
		{EventKey: "calendar-before", APIGroupKey: "provider-a", Timestamp: dayStart.Add(-30 * time.Second), InputTokens: 1000, TotalTokens: 1000},
		{EventKey: "calendar-start", APIGroupKey: "provider-a", Timestamp: dayStart.Add(30 * time.Second), InputTokens: 10, TotalTokens: 10},
		{EventKey: "calendar-middle", APIGroupKey: "provider-a", Timestamp: dayStart.Add(12 * time.Hour), Failed: true, OutputTokens: 20, TotalTokens: 20},
		{EventKey: "calendar-end", APIGroupKey: "provider-a", Timestamp: dayEnd.Add(-30 * time.Second), CacheReadTokens: 30, TotalTokens: 30},
		{EventKey: "calendar-after", APIGroupKey: "provider-a", Timestamp: dayEnd.Add(30 * time.Second), InputTokens: 2000, TotalTokens: 2000},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert calendar Activity events: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, dayEnd.Add(time.Hour)); err != nil {
		t.Fatalf("aggregate calendar Activity events: %v", err)
	}
	// 自然日查询必须完全复用 Activity 聚合行；删除 raw events 后结果仍应完整。
	if err := db.Where("event_key LIKE ?", "calendar-%").Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("remove raw calendar Activity events: %v", err)
	}

	filterEnd := dayEnd.Add(-time.Nanosecond)
	queryNow := dayEnd.Add(12 * time.Hour)
	activity, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetUsageActivity(context.Background(), servicedto.UsageFilter{
		Range:          "yesterday",
		RangeUnit:      "day",
		RangeCount:     1,
		StartTime:      &dayStart,
		EndTime:        &filterEnd,
		ActivityWindow: servicedto.UsageActivityWindow("yesterday"),
		QueryNow:       &queryNow,
	})
	if err != nil {
		t.Fatalf("GetUsageActivity returned error: %v", err)
	}
	if activity.Window != servicedto.UsageActivityWindowDay || activity.Grain != string(entities.UsageActivityGrainShort) {
		t.Fatalf("unexpected calendar Activity identity: window=%q grain=%q", activity.Window, activity.Grain)
	}
	if !activity.WindowStart.Equal(dayStart) || !activity.WindowEnd.Equal(dayEnd) {
		t.Fatalf("unexpected calendar Activity window: %s..%s", activity.WindowStart, activity.WindowEnd)
	}
	if activity.Rows != 7 || activity.Columns != 52 || len(activity.Blocks) != repository.UsageActivityHeatmapBlocks {
		t.Fatalf("unexpected calendar Activity shape: rows=%d columns=%d blocks=%d", activity.Rows, activity.Columns, len(activity.Blocks))
	}
	if activity.TotalSuccess != 2 || activity.TotalFailure != 1 || activity.InputTokens != 10 || activity.OutputTokens != 20 || activity.CacheReadTokens != 30 || activity.TotalTokens != 60 {
		t.Fatalf("calendar Activity included events outside the selected day: %+v", activity)
	}
	var blockSuccess, blockFailure, blockTokens int64
	for index, block := range activity.Blocks {
		if index > 0 && !activity.Blocks[index-1].EndTime.Equal(block.StartTime) {
			t.Fatalf("calendar Activity blocks %d and %d are not contiguous", index-1, index)
		}
		blockSuccess += block.Success
		blockFailure += block.Failure
		blockTokens += block.TotalTokens
	}
	if blockSuccess != activity.TotalSuccess || blockFailure != activity.TotalFailure || blockTokens != activity.TotalTokens {
		t.Fatalf("calendar Activity header and blocks disagree: header=%d/%d/%d blocks=%d/%d/%d", activity.TotalSuccess, activity.TotalFailure, activity.TotalTokens, blockSuccess, blockFailure, blockTokens)
	}
}

func TestUsageActivityTodayKeepsFullDayAxisButExcludesFutureData(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	db := openUsageActivityServiceDatabase(t)
	dayStart := time.Date(2026, 7, 20, 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	queryNow := dayStart.Add(12 * time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "today-past", APIGroupKey: "provider-a", Timestamp: queryNow.Add(-time.Minute), InputTokens: 10, TotalTokens: 10},
		{EventKey: "today-future", APIGroupKey: "provider-a", Timestamp: queryNow.Add(time.Minute), InputTokens: 1000, TotalTokens: 1000},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert Today Activity events: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, queryNow); err != nil {
		t.Fatalf("aggregate Today Activity events: %v", err)
	}

	filterEnd := dayEnd.Add(-time.Nanosecond)
	activity, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetUsageActivity(context.Background(), servicedto.UsageFilter{
		Range:          "today",
		RangeUnit:      "day",
		RangeCount:     1,
		StartTime:      &dayStart,
		EndTime:        &filterEnd,
		ActivityWindow: servicedto.UsageActivityWindow("today"),
		QueryNow:       &queryNow,
	})
	if err != nil {
		t.Fatalf("GetUsageActivity returned error: %v", err)
	}
	if !activity.WindowStart.Equal(dayStart) || !activity.WindowEnd.Equal(dayEnd) {
		t.Fatalf("Today Activity did not keep the full calendar axis: %s..%s", activity.WindowStart, activity.WindowEnd)
	}
	if activity.TotalSuccess != 1 || activity.InputTokens != 10 || activity.TotalTokens != 10 {
		t.Fatalf("Today Activity included data after QueryNow: %+v", activity)
	}
	for _, block := range activity.Blocks {
		if !block.StartTime.Before(queryNow) && block.Success+block.Failure != 0 {
			t.Fatalf("Today Activity populated future block %+v", block)
		}
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

	activity, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetUsageActivity(context.Background(), servicedto.UsageFilter{
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
