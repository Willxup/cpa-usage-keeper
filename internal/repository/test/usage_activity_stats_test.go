package test

import (
	"context"
	"slices"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

func TestAggregateUsageActivityStatsUsesIndependentCheckpointAndCanonicalTokens(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	// fresh database 同时包含现有 Overview 表和新的 Activity 表。
	db := openTestDatabase(t)
	events := []entities.UsageEvent{
		{EventKey: "activity-1", APIGroupKey: " provider-a ", Model: "model-a", Timestamp: now.Add(-time.Hour), Failed: false, InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 900, CacheReadTokens: 10, CacheCreationTokens: 3, TotalTokens: 138},
		{EventKey: "activity-2", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-30 * time.Minute), Failed: true, InputTokens: 200, OutputTokens: 30, ReasoningTokens: 6, CachedTokens: 800, CacheReadTokens: 20, CacheCreationTokens: 4, TotalTokens: 260},
		{EventKey: "activity-3", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-4 * 24 * time.Hour), Failed: false, InputTokens: 300, OutputTokens: 40, ReasoningTokens: 7, CachedTokens: 700, CacheReadTokens: 30, CacheCreationTokens: 5, TotalTokens: 382},
		{EventKey: "activity-4", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-10 * 24 * time.Hour), Failed: false, InputTokens: 400, OutputTokens: 50, ReasoningTokens: 8, CachedTokens: 600, CacheReadTokens: 40, CacheCreationTokens: 6, TotalTokens: 504},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}

	// Overview 行使用无关 sentinel，证明 Activity 只推进通用表中的自己那一行。
	overviewCheckpoint := entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 777}
	if err := db.Create(&overviewCheckpoint).Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}

	pending, err := repository.HasPendingUsageActivityAggregation(context.Background(), db)
	if err != nil {
		t.Fatalf("HasPendingUsageActivityAggregation returned error: %v", err)
	}
	if !pending {
		t.Fatal("expected activity aggregation to be pending")
	}

	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageActivityStats returned error: %v", err)
	}

	assertRepositoryUsageActivityTotals(t, db, entities.UsageActivityGrainShort, repositoryUsageActivityTotals{Success: 1, Failure: 1, Input: 300, Output: 50, Reasoning: 11, CacheRead: 30, CacheCreation: 7, Total: 398})
	assertRepositoryUsageActivityTotals(t, db, entities.UsageActivityGrainMedium, repositoryUsageActivityTotals{Success: 2, Failure: 1, Input: 600, Output: 90, Reasoning: 18, CacheRead: 60, CacheCreation: 12, Total: 780})
	assertRepositoryUsageActivityTotals(t, db, entities.UsageActivityGrainLong, repositoryUsageActivityTotals{Success: 3, Failure: 1, Input: 1000, Output: 140, Reasoning: 26, CacheRead: 100, CacheCreation: 18, Total: 1284})
	assertRepositoryUsageActivityTotals(t, db, entities.UsageActivityGrainDaily, repositoryUsageActivityTotals{Success: 3, Failure: 1, Input: 1000, Output: 140, Reasoning: 26, CacheRead: 100, CacheCreation: 18, Total: 1284})

	// Activity checkpoint 必须独立推进到最后一个 event ID。
	var activityCheckpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointActivity).Take(&activityCheckpoint).Error; err != nil {
		t.Fatalf("load activity checkpoint: %v", err)
	}
	if activityCheckpoint.LastAggregatedUsageEventID != 4 {
		t.Fatalf("expected activity checkpoint 4, got %+v", activityCheckpoint)
	}
	var unchangedOverview entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&unchangedOverview).Error; err != nil {
		t.Fatalf("load overview checkpoint: %v", err)
	}
	if unchangedOverview.LastAggregatedUsageEventID != 777 {
		t.Fatalf("activity changed overview checkpoint: %+v", unchangedOverview)
	}

	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("rerun AggregateUsageActivityStats: %v", err)
	}
	assertRepositoryUsageActivityTotals(t, db, entities.UsageActivityGrainDaily, repositoryUsageActivityTotals{Success: 3, Failure: 1, Input: 1000, Output: 140, Reasoning: 26, CacheRead: 100, CacheCreation: 18, Total: 1284})

	pending, err = repository.HasPendingUsageActivityAggregation(context.Background(), db)
	if err != nil {
		t.Fatalf("second HasPendingUsageActivityAggregation returned error: %v", err)
	}
	if pending {
		t.Fatal("expected activity aggregation to be caught up")
	}
}

func TestAggregateUsageActivityStatsStoresDSTFallbackBucketByInstantOrder(t *testing.T) {
	fallbackBucket := activityFallbackBucket(t)
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{
		EventKey: "activity-dst-fallback", APIGroupKey: "provider-a",
		Timestamp: fallbackBucket.Start.Add(time.Second), InputTokens: 10, TotalTokens: 10,
	}}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert fallback usage event: %v", err)
	}

	err := repository.AggregateUsageActivityStats(context.Background(), db, fallbackBucket.End.Add(time.Hour))

	if err != nil {
		t.Fatalf("AggregateUsageActivityStats returned error: %v", err)
	}
	var row entities.UsageActivityStat
	if err := db.Where("grain = ? AND api_group_key = ?", entities.UsageActivityGrainShort, "provider-a").Take(&row).Error; err != nil {
		t.Fatalf("load fallback Activity row: %v", err)
	}
	if !row.BucketStart.Equal(fallbackBucket.Start) || !row.BucketEnd.Equal(fallbackBucket.End) {
		t.Fatalf("unexpected fallback Activity bounds: got=%s..%s want=%s..%s", row.BucketStart, row.BucketEnd, fallbackBucket.Start, fallbackBucket.End)
	}
	var storedBounds struct {
		BucketStart string
		BucketEnd   string
	}
	// CAST 绕过 SQLite driver 对 datetime 的 time.Time 格式化，读取数据库实际保存的 TEXT。
	if err := db.Table("usage_activity_stats").Select("CAST(bucket_start AS TEXT) AS bucket_start, CAST(bucket_end AS TEXT) AS bucket_end").Where("id = ?", row.ID).Scan(&storedBounds).Error; err != nil {
		t.Fatalf("load raw fallback Activity bounds: %v", err)
	}
	if storedBounds.BucketStart != timeutil.FormatSortableStorageTime(fallbackBucket.Start) || storedBounds.BucketEnd != timeutil.FormatSortableStorageTime(fallbackBucket.End) {
		t.Fatalf("unexpected stored fallback bounds: got=%s..%s", storedBounds.BucketStart, storedBounds.BucketEnd)
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointActivity).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load fallback Activity checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 1 {
		t.Fatalf("expected fallback Activity checkpoint 1, got %+v", checkpoint)
	}
}

func TestCleanupUsageActivityStatsUsesPerGrainBucketEndRetention(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)

	// daily-old 必须永久保留，其余 old-* 必须按各自 grain 删除。
	rows := []entities.UsageActivityStat{
		usageActivityCleanupRow(entities.UsageActivityGrainShort, "short-old", now.Add(-4*24*time.Hour), now.Add(-4*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainShort, "short-fresh", now.Add(-2*24*time.Hour), now.Add(-2*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainMedium, "medium-old", now.Add(-9*24*time.Hour), now.Add(-9*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainMedium, "medium-fresh", now.Add(-7*24*time.Hour), now.Add(-7*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainLong, "long-old", now.Add(-32*24*time.Hour), now.Add(-32*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainLong, "long-fresh", now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour+time.Minute)),
		usageActivityCleanupRow(entities.UsageActivityGrainDaily, "daily-old", now.Add(-1000*24*time.Hour), now.Add(-999*24*time.Hour)),
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed activity cleanup rows: %v", err)
	}

	if err := repository.CleanupUsageActivityStats(db, now); err != nil {
		t.Fatalf("CleanupUsageActivityStats returned error: %v", err)
	}
	var remaining []string
	if err := db.Model(&entities.UsageActivityStat{}).Order("api_group_key asc").Pluck("api_group_key", &remaining).Error; err != nil {
		t.Fatalf("load remaining activity rows: %v", err)
	}
	want := []string{"daily-old", "long-fresh", "medium-fresh", "short-fresh"}
	if !slices.Equal(remaining, want) {
		t.Fatalf("unexpected remaining activity rows: got=%v want=%v", remaining, want)
	}
}

func TestCleanupUsageActivityStatsOrdersDSTFallbackCutoffByInstant(t *testing.T) {
	fallbackBucket := activityFallbackBucket(t)
	db := openTestDatabase(t)
	row := entities.UsageActivityStat{
		Grain: entities.UsageActivityGrainShort, BucketStart: fallbackBucket.Start, BucketEnd: fallbackBucket.End,
		APIGroupKey: "fallback-cleanup", SuccessCount: 1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed fallback Activity cleanup row: %v", err)
	}
	// cutoff 比 bucket_end 晚一秒，因此该 short row 已经完整过期。
	cleanupNow := fallbackBucket.End.Add(time.Second).Add(3 * 24 * time.Hour)

	if err := repository.CleanupUsageActivityStats(db, cleanupNow); err != nil {
		t.Fatalf("CleanupUsageActivityStats returned error: %v", err)
	}

	var remaining int64
	if err := db.Model(&entities.UsageActivityStat{}).Where("api_group_key = ?", "fallback-cleanup").Count(&remaining).Error; err != nil {
		t.Fatalf("count fallback Activity cleanup rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected expired fallback Activity row to be deleted, got %d", remaining)
	}
}

type repositoryUsageActivityTotals struct {
	Success       int64
	Failure       int64
	Input         int64
	Output        int64
	Reasoning     int64
	CacheRead     int64
	CacheCreation int64
	Total         int64
}

func assertRepositoryUsageActivityTotals(t *testing.T, db *gorm.DB, grain entities.UsageActivityGrain, want repositoryUsageActivityTotals) {
	// 此 helper 汇总稀疏 bucket，只比较每个 grain 的最终累计效果。
	t.Helper()
	var got repositoryUsageActivityTotals
	if err := db.Model(&entities.UsageActivityStat{}).
		Select(`COALESCE(SUM(success_count), 0) AS success,
			COALESCE(SUM(failure_count), 0) AS failure,
			COALESCE(SUM(input_tokens), 0) AS input,
			COALESCE(SUM(output_tokens), 0) AS output,
			COALESCE(SUM(reasoning_tokens), 0) AS reasoning,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation,
			COALESCE(SUM(total_tokens), 0) AS total`).
		Where("grain = ?", grain).
		Scan(&got).Error; err != nil {
		t.Fatalf("sum %s activity rows: %v", grain, err)
	}
	if got != want {
		t.Fatalf("unexpected %s totals: got=%+v want=%+v", grain, got, want)
	}
}

func usageActivityCleanupRow(grain entities.UsageActivityGrain, apiGroupKey string, start, end time.Time) entities.UsageActivityStat {
	// cleanup fixture 只需要唯一边界和一个非零请求计数。
	return entities.UsageActivityStat{Grain: grain, BucketStart: timeutil.NormalizeStorageTime(start), BucketEnd: timeutil.NormalizeStorageTime(end), APIGroupKey: apiGroupKey, SuccessCount: 1}
}

func activityFallbackBucket(t *testing.T) repository.UsageActivityBucket {
	t.Helper()
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load DST location: %v", err)
	}
	previousLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	referenceInstant, err := time.Parse(time.RFC3339, "2026-11-01T01:15:00-05:00")
	if err != nil {
		t.Fatalf("parse fallback reference: %v", err)
	}
	buckets, err := repository.UsageActivityWindowEndingAt(entities.UsageActivityGrainShort, referenceInstant.In(location))
	if err != nil {
		t.Fatalf("resolve fallback Activity window: %v", err)
	}
	var fallbackBucket repository.UsageActivityBucket
	for _, bucket := range buckets {
		startLocal := bucket.Start.In(location)
		endLocal := bucket.End.In(location)
		if startLocal.Format("2006-01-02 15:04:05") >= endLocal.Format("2006-01-02 15:04:05") {
			fallbackBucket = bucket
			break
		}
	}
	if fallbackBucket.Start.IsZero() {
		t.Fatal("expected one short bucket to cross the DST fallback boundary")
	}
	return fallbackBucket
}
