package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func TestAggregateUsageOverviewStatsAggregatesIncrementallyAndIdempotently(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	insertUsageOverviewAggregationEvents(t, db, []entities.UsageEvent{
		usageOverviewAggregationEvent("event-1", " api-a ", " claude-sonnet ", time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC), false, 10, 20, 3, 4, 5, 6, 37),
		usageOverviewAggregationEvent("event-2", "api-a", "claude-sonnet", time.Date(2026, 5, 14, 10, 55, 0, 0, time.UTC), true, 7, 8, 0, 1, 2, 3, 16),
	})

	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}
	assertUsageOverviewStat(t, db, "hourly", time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC), "api-a", "claude-sonnet", "", "", overviewStatValues{2, 1, 1, 17, 28, 3, 5, 7, 9, 53})
	assertUsageOverviewStat(t, db, "daily", usageOverviewAggregationDayBucket(2026, 5, 14), "api-a", "claude-sonnet", "", "", overviewStatValues{2, 1, 1, 17, 28, 3, 5, 7, 9, 53})
	assertUsageOverviewCheckpoint(t, db, 2)

	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatalf("second AggregateUsageOverviewStats returned error: %v", err)
	}
	assertUsageOverviewStat(t, db, "hourly", time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC), "api-a", "claude-sonnet", "", "", overviewStatValues{2, 1, 1, 17, 28, 3, 5, 7, 9, 53})

	insertUsageOverviewAggregationEvents(t, db, []entities.UsageEvent{
		usageOverviewAggregationEvent("event-3", "api-a", "claude-sonnet", time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC), false, 1, 2, 3, 4, 5, 6, 10),
	})
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("delta AggregateUsageOverviewStats returned error: %v", err)
	}
	assertUsageOverviewStat(t, db, "hourly", time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC), "api-a", "claude-sonnet", "", "", overviewStatValues{1, 1, 0, 1, 2, 3, 4, 5, 6, 10})
	assertUsageOverviewStat(t, db, "daily", usageOverviewAggregationDayBucket(2026, 5, 13), "api-a", "claude-sonnet", "", "", overviewStatValues{1, 1, 0, 1, 2, 3, 4, 5, 6, 10})
	assertUsageOverviewCheckpoint(t, db, 3)
}

func TestAggregateUsageOverviewStatsSplitsHourlyAndDailyByAuthIndexAndModelAlias(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	modelAlias := "alias-a"
	blankAlias := "  "

	eventTime := time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		usageOverviewAggregationEvent("event-auth-1", "api-a", "claude-sonnet", eventTime, false, 10, 20, 0, 0, 0, 0, 30),
		usageOverviewAggregationEvent("event-auth-2", "api-a", "claude-sonnet", eventTime, false, 1, 2, 0, 0, 0, 0, 3),
		usageOverviewAggregationEvent("event-auth-3", "api-a", "claude-sonnet", eventTime, false, 3, 4, 0, 0, 0, 0, 7),
		usageOverviewAggregationEvent("event-blank", " ", " ", now.Add(-time.Minute), true, 1, 1, 0, 0, 0, 0, 2),
	}
	events[0].AuthIndex = "auth-a"
	events[0].ModelAlias = &modelAlias
	events[1].AuthIndex = "auth-b"
	events[1].ModelAlias = &modelAlias
	events[2].AuthIndex = "auth-a"
	events[2].ModelAlias = &blankAlias
	insertUsageOverviewAggregationEvents(t, db, events)

	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	bucket := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	dayBucket := usageOverviewAggregationDayBucket(2026, 5, 14)
	assertUsageOverviewStat(t, db, "hourly", bucket, "api-a", "claude-sonnet", "auth-a", "alias-a", overviewStatValues{1, 1, 0, 10, 20, 0, 0, 0, 0, 30})
	assertUsageOverviewStat(t, db, "hourly", bucket, "api-a", "claude-sonnet", "auth-b", "alias-a", overviewStatValues{1, 1, 0, 1, 2, 0, 0, 0, 0, 3})
	assertUsageOverviewStat(t, db, "hourly", bucket, "api-a", "claude-sonnet", "auth-a", "", overviewStatValues{1, 1, 0, 3, 4, 0, 0, 0, 0, 7})
	assertUsageOverviewStat(t, db, "daily", dayBucket, "api-a", "claude-sonnet", "auth-a", "alias-a", overviewStatValues{1, 1, 0, 10, 20, 0, 0, 0, 0, 30})

	assertUsageOverviewStat(t, db, "hourly", time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC), "unknown", "unknown", "", "", overviewStatValues{1, 0, 1, 1, 1, 0, 0, 0, 0, 2})

	// Overview 聚合拆分后不得顺带写 Activity；Activity 由自己的独立 checkpoint 处理。
	var activityCount int64
	if err := db.Model(&entities.UsageActivityStat{}).Count(&activityCount).Error; err != nil {
		t.Fatalf("count activity rows: %v", err)
	}
	if activityCount != 0 {
		t.Fatalf("expected overview aggregation not to write activity rows, got %d", activityCount)
	}
}

func usageOverviewAggregationEvent(eventKey, apiGroupKey, model string, timestamp time.Time, failed bool, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheReadTokens, cacheCreationTokens, totalTokens int64) entities.UsageEvent {
	return entities.UsageEvent{
		EventKey:            eventKey,
		APIGroupKey:         apiGroupKey,
		Model:               model,
		Timestamp:           timestamp,
		Failed:              failed,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		ReasoningTokens:     reasoningTokens,
		CachedTokens:        cachedTokens,
		CacheReadTokens:     cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
		TotalTokens:         totalTokens,
	}
}

func usageOverviewAggregationDayBucket(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func insertUsageOverviewAggregationEvents(t *testing.T, db *gorm.DB, events []entities.UsageEvent) {
	t.Helper()
	inserted, deduped, err := repository.InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != len(events) || deduped != 0 {
		t.Fatalf("expected inserted=%d deduped=0, got inserted=%d deduped=%d", len(events), inserted, deduped)
	}
}

type overviewStatValues struct {
	RequestCount, SuccessCount, FailureCount                        int64
	InputTokens, OutputTokens, ReasoningTokens                      int64
	CachedTokens, CacheReadTokens, CacheCreationTokens, TotalTokens int64
}

func assertUsageOverviewStat(t *testing.T, db *gorm.DB, grain string, bucketStart time.Time, apiGroupKey, model, authIndex, modelAlias string, want overviewStatValues) {
	t.Helper()
	var got overviewStatValues
	if err := db.Table("usage_overview_"+grain+"_stats").Where("bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ?", timeutil.FormatStorageTime(bucketStart), apiGroupKey, model, authIndex, modelAlias).Take(&got).Error; err != nil {
		t.Fatalf("load %s stat %s/%s/%s/%s/%s: %v", grain, bucketStart, apiGroupKey, model, authIndex, modelAlias, err)
	}
	if got != want {
		t.Fatalf("unexpected %s values: got=%+v want=%+v", grain, got, want)
	}
}

func assertUsageOverviewCheckpoint(t *testing.T, db *gorm.DB, wantLastID int64) {
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).First(&checkpoint).Error; err != nil {
		t.Fatalf("load overview checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != wantLastID || checkpoint.StatsUpdatedAt == nil {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
}
