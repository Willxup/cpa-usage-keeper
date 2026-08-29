package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const usageActivityShortAlignmentMigrationVersion = "20260722_align_usage_activity_short"

func TestUsageActivityShortAlignmentMigrationRebuildsOnlyCheckpointedShortRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	db := openUsageActivityMigrationDatabase(t, "usage-activity-short-alignment.db")
	if err := db.AutoMigrate(&entities.UsageEvent{}, &entities.UsageActivityStat{}, &entities.UsageActivityAggregationCheckpoint{}); err != nil {
		t.Fatalf("create Activity schema: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageActivityShortAlignmentMigrationVersion).Error; err != nil {
		t.Fatalf("enable short alignment migration: %v", err)
	}

	now := timeutil.NormalizeStorageTime(time.Now()).Truncate(time.Second)
	previousDayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -1)
	events := []entities.UsageEvent{
		usageActivityMigrationEvent(1, "provider-a", previousDayStart.Add(30*time.Second), false, 10, 20, 3, 999, 4, 5, 42),
		usageActivityMigrationEvent(2, "provider-a", now.Add(-30*time.Minute), true, 100, 200, 30, 888, 40, 50, 420),
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	legacyStart := previousDayStart.Add(17 * time.Second)
	rows := []entities.UsageActivityStat{
		{Grain: entities.UsageActivityGrainShort, BucketStart: legacyStart, BucketEnd: legacyStart.Add(time.Minute), APIGroupKey: "provider-a", SuccessCount: 99, TotalTokens: 999},
		{Grain: entities.UsageActivityGrainMedium, BucketStart: now.Add(-time.Hour), BucketEnd: now, APIGroupKey: "sentinel", SuccessCount: 7, TotalTokens: 70},
		{Grain: entities.UsageActivityGrainLong, BucketStart: now.Add(-2 * time.Hour), BucketEnd: now, APIGroupKey: "sentinel", FailureCount: 8, TotalTokens: 80},
		{Grain: entities.UsageActivityGrainDaily, BucketStart: previousDayStart, BucketEnd: previousDayStart.AddDate(0, 0, 1), APIGroupKey: "sentinel", SuccessCount: 9, TotalTokens: 90},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed Activity rows: %v", err)
	}
	checkpointTime := now.Add(-time.Minute)
	checkpoint := entities.UsageActivityAggregationCheckpoint{Name: "activity", LastAggregatedUsageEventID: 1, StatsUpdatedAt: &checkpointTime}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatalf("seed Activity checkpoint: %v", err)
	}

	beforeMedium := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainMedium)
	beforeLong := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainLong)
	beforeDaily := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainDaily)
	beforeCheckpoint := loadUsageActivityCheckpoint(t, db)

	if err := migration.Run(db); err != nil {
		t.Fatalf("run short alignment migration: %v", err)
	}

	assertUsageActivityTotals(t, db, entities.UsageActivityGrainShort, usageActivityTotals{
		SuccessCount: 1, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 3,
		CacheReadTokens: 4, CacheCreationTokens: 5, TotalTokens: 42,
	})
	shortRows := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainShort)
	for _, row := range shortRows {
		bucket, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, row.BucketStart)
		if err != nil {
			t.Fatalf("resolve rebuilt short bucket: %v", err)
		}
		if !row.BucketStart.Equal(bucket.Start) || !row.BucketEnd.Equal(bucket.End) {
			t.Fatalf("short row is not midnight-aligned: row=%+v bucket=%+v", row, bucket)
		}
	}
	if after := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainMedium); !reflect.DeepEqual(after, beforeMedium) {
		t.Fatalf("medium rows changed:\n before=%+v\n after=%+v", beforeMedium, after)
	}
	if after := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainLong); !reflect.DeepEqual(after, beforeLong) {
		t.Fatalf("long rows changed:\n before=%+v\n after=%+v", beforeLong, after)
	}
	if after := loadUsageActivityRowsByGrain(t, db, entities.UsageActivityGrainDaily); !reflect.DeepEqual(after, beforeDaily) {
		t.Fatalf("daily rows changed:\n before=%+v\n after=%+v", beforeDaily, after)
	}
	if after := loadUsageActivityCheckpoint(t, db); !reflect.DeepEqual(after, beforeCheckpoint) {
		t.Fatalf("Activity checkpoint changed:\n before=%+v\n after=%+v", beforeCheckpoint, after)
	}

	// 新运行时只读取通用 checkpoint；本历史 migration 用例在增量调用前手工映射旧 Activity 水位。
	if err := db.AutoMigrate(&entities.UsageAggregationCheckpoint{}); err != nil {
		t.Fatalf("create common aggregation checkpoint table: %v", err)
	}
	commonCheckpoint := entities.UsageAggregationCheckpoint{
		Name:                       entities.UsageAggregationCheckpointActivity,
		LastAggregatedUsageEventID: checkpoint.LastAggregatedUsageEventID,
		StatsUpdatedAt:             checkpoint.StatsUpdatedAt,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := db.Create(&commonCheckpoint).Error; err != nil {
		t.Fatalf("seed common Activity checkpoint: %v", err)
	}

	// checkpoint 之后的 event 由正常增量补齐，short 最终只能累计一次。
	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("catch up Activity after short alignment: %v", err)
	}
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainShort, usageActivityTotals{
		SuccessCount: 1, FailureCount: 1, InputTokens: 110, OutputTokens: 220, ReasoningTokens: 33,
		CacheReadTokens: 44, CacheCreationTokens: 55, TotalTokens: 462,
	})

	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", usageActivityShortAlignmentMigrationVersion).Count(&applied).Error; err != nil {
		t.Fatalf("count short alignment migration: %v", err)
	}
	if applied != 1 {
		t.Fatalf("short alignment migration applied=%d, want 1", applied)
	}
	beforeRerun := loadUsageActivityRows(t, db)
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun short alignment migration: %v", err)
	}
	if afterRerun := loadUsageActivityRows(t, db); !reflect.DeepEqual(afterRerun, beforeRerun) {
		t.Fatalf("Activity rows changed after idempotent rerun:\n before=%+v\n after=%+v", beforeRerun, afterRerun)
	}
}

func loadUsageActivityRowsByGrain(t *testing.T, db *gorm.DB, grain entities.UsageActivityGrain) []entities.UsageActivityStat {
	t.Helper()
	var rows []entities.UsageActivityStat
	if err := db.Where("grain = ?", grain).Order("bucket_start asc, api_group_key asc").Find(&rows).Error; err != nil {
		t.Fatalf("load %s Activity rows: %v", grain, err)
	}
	return rows
}
