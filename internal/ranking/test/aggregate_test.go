package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
)

func TestAggregatorBuildsOneUTCDayFromUsageEvents(t *testing.T) {
	db := openRankingDatabase(t)
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	ttft100 := int64(100)
	ttft200 := int64(200)
	ttftOutside := int64(999)
	events := []entities.UsageEvent{
		{EventKey: "before", Timestamp: start.Add(-time.Nanosecond), TTFTMS: &ttftOutside, LatencyMS: 999, InputTokens: 999, TotalTokens: 999},
		{EventKey: "valid-a", Timestamp: start.Add(time.Minute), TTFTMS: &ttft100, LatencyMS: 500, InputTokens: 10, OutputTokens: 1, ReasoningTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4, TotalTokens: 100},
		{EventKey: "valid-b", Timestamp: start.Add(4 * time.Minute), TTFTMS: &ttft200, LatencyMS: 600, InputTokens: 20, OutputTokens: 2, ReasoningTokens: 3, CacheReadTokens: 4, CacheCreationTokens: 5, TotalTokens: 200},
		{EventKey: "failed", Timestamp: start.Add(6 * time.Minute), Failed: true, TTFTMS: &ttftOutside, LatencyMS: 900, InputTokens: 30, OutputTokens: 3, ReasoningTokens: 4, CacheReadTokens: 5, CacheCreationTokens: 6, TotalTokens: 400},
		{EventKey: "no-ttft", Timestamp: start.Add(6*time.Minute + 30*time.Second), LatencyMS: 700, InputTokens: 40, OutputTokens: 4, ReasoningTokens: 5, CacheReadTokens: 6, CacheCreationTokens: 7, TotalTokens: 10},
		{EventKey: "at-end", Timestamp: end, TTFTMS: &ttftOutside, LatencyMS: 999, InputTokens: 999, TotalTokens: 999},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	aggregator := ranking.NewAggregator(db)
	metrics, err := aggregator.AggregateDay(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateDay returned error: %v", err)
	}
	want := ranking.Metrics{
		RequestCount: 4, SuccessCount: 3, FailureCount: 1,
		InputTokens: 100, OutputTokens: 10, ReasoningTokens: 14,
		CacheReadTokens: 18, CacheCreationTokens: 22, TotalTokens: 710,
		TTFTSumMS: 300, TTFTSampleCount: 2,
		LatencySumMS: 1100, LatencySampleCount: 2,
		Peak5MRequestCount: 2, Peak5MTotalTokens: 410,
	}
	if metrics != want {
		t.Fatalf("unexpected metrics:\nwant=%+v\n got=%+v", want, metrics)
	}

	latestID, err := aggregator.LatestEventID(context.Background(), start, end)
	if err != nil {
		t.Fatalf("LatestEventID returned error: %v", err)
	}
	if latestID != events[len(events)-2].ID {
		t.Fatalf("expected latest in-range event ID %d, got %d", events[len(events)-2].ID, latestID)
	}
}

func TestAggregatorReturnsZeroMetricsForEmptyDay(t *testing.T) {
	aggregator := ranking.NewAggregator(openRankingDatabase(t))
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)

	metrics, err := aggregator.AggregateDay(context.Background(), start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("AggregateDay returned error: %v", err)
	}
	if metrics != (ranking.Metrics{}) {
		t.Fatalf("expected zero metrics, got %+v", metrics)
	}
}

func TestAggregatorReturnsZeroMetricsAtExactUTCMidnight(t *testing.T) {
	aggregator := ranking.NewAggregator(openRankingDatabase(t))
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)

	metrics, err := aggregator.AggregateDay(context.Background(), start, start)
	if err != nil {
		t.Fatalf("AggregateDay at midnight returned error: %v", err)
	}
	if metrics != (ranking.Metrics{}) {
		t.Fatalf("expected zero midnight metrics, got %+v", metrics)
	}
}

func TestAggregatorAllowsCurrentUTCDayPrefix(t *testing.T) {
	db := openRankingDatabase(t)
	aggregator := ranking.NewAggregator(db)
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{EventKey: "included", Timestamp: start.Add(time.Minute), TotalTokens: 12},
		{EventKey: "excluded", Timestamp: start.Add(6 * time.Minute), TotalTokens: 99},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	metrics, err := aggregator.AggregateDay(context.Background(), start, start.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("AggregateDay current prefix returned error: %v", err)
	}
	if metrics.RequestCount != 1 || metrics.TotalTokens != 12 {
		t.Fatalf("unexpected partial-day metrics: %+v", metrics)
	}
}

func TestAggregatorKeepsFractionalCurrentDayBoundary(t *testing.T) {
	db := openRankingDatabase(t)
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	end := start.Add(5*time.Minute + 500*time.Millisecond)
	events := []entities.UsageEvent{
		{EventKey: "before-fractional-end", Timestamp: end.Add(-100 * time.Millisecond), TotalTokens: 12},
		{EventKey: "after-fractional-end", Timestamp: end.Add(100 * time.Millisecond), TotalTokens: 99},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed fractional boundary events: %v", err)
	}

	metrics, err := ranking.NewAggregator(db).AggregateDay(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateDay fractional boundary returned error: %v", err)
	}
	if metrics.RequestCount != 1 || metrics.TotalTokens != 12 {
		t.Fatalf("unexpected fractional boundary metrics: %+v", metrics)
	}
}

func TestAggregatorAcceptsCenterCalendarDayRange(t *testing.T) {
	db := openRankingDatabase(t)
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Shanghai location: %v", err)
	}
	keeperLocation, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load Keeper location: %v", err)
	}
	previousLocal := time.Local
	time.Local = keeperLocation
	t.Cleanup(func() { time.Local = previousLocal })
	start := time.Date(2026, 7, 23, 0, 0, 0, 0, shanghai)
	end := start.AddDate(0, 0, 1)
	if err := db.Create(&entities.UsageEvent{EventKey: "shanghai-day", Timestamp: start.Add(time.Minute), TotalTokens: 12}).Error; err != nil {
		t.Fatalf("seed Shanghai calendar day event: %v", err)
	}

	metrics, err := ranking.NewAggregator(db).AggregateDay(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateDay rejected center calendar day range: %v", err)
	}
	if metrics.RequestCount != 1 || metrics.TotalTokens != 12 {
		t.Fatalf("unexpected Shanghai calendar day metrics: %+v", metrics)
	}
}

func TestAggregatorUsesInstantOrderAcrossKeeperDSTRollback(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Shanghai location: %v", err)
	}
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatalf("load Sydney location: %v", err)
	}
	previousLocal := time.Local
	time.Local = sydney
	t.Cleanup(func() { time.Local = previousLocal })

	db := openRankingDatabase(t)
	start := time.Date(2026, 4, 5, 0, 0, 0, 0, shanghai)
	end := start.AddDate(0, 0, 1)
	events := []entities.UsageEvent{
		// 两个事件在 Sydney 都显示为 02:30，但 +11:00 的事件实际早于中心日界线。
		{EventKey: "before-dst-boundary", Timestamp: start.Add(-30 * time.Minute), TotalTokens: 99},
		{EventKey: "inside-dst-boundary", Timestamp: start.Add(30 * time.Minute), TotalTokens: 12},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed DST rollback events: %v", err)
	}

	aggregator := ranking.NewAggregator(db)
	metrics, err := aggregator.AggregateDay(context.Background(), start, end)
	if err != nil {
		t.Fatalf("AggregateDay returned error: %v", err)
	}
	if metrics.RequestCount != 1 || metrics.TotalTokens != 12 {
		t.Fatalf("DST rollback crossed the center boundary: %+v", metrics)
	}
	latestID, err := aggregator.LatestEventID(context.Background(), start, end)
	if err != nil {
		t.Fatalf("LatestEventID returned error: %v", err)
	}
	if latestID != events[1].ID {
		t.Fatalf("LatestEventID = %d, want %d", latestID, events[1].ID)
	}
}

func TestAggregatorRejectsRangeOutsideOneCenterDay(t *testing.T) {
	aggregator := ranking.NewAggregator(openRankingDatabase(t))
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)

	if _, err := aggregator.AggregateDay(context.Background(), start.Add(time.Minute), start.Add(time.Hour)); err == nil {
		t.Fatal("expected non-midnight start to be rejected")
	}
	if _, err := aggregator.AggregateDay(context.Background(), start, start.Add(25*time.Hour)); err == nil {
		t.Fatal("expected multi-day range to be rejected")
	}
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	if _, err := aggregator.AggregateDay(context.Background(), start.In(shanghai), start.Add(time.Hour).In(shanghai)); err == nil {
		t.Fatal("expected non-UTC range to be rejected")
	}
}

func TestAggregatorBypassesOccupiedSQLiteWriter(t *testing.T) {
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ranking-reader.db")})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		if sqlDB, err := reader.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	start := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageEvent{EventKey: "reader-route", Timestamp: start.Add(time.Minute), TotalTokens: 12}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	writeSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer pool: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("occupy writer connection: %v", err)
	}
	defer heldWriter.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	metrics, err := ranking.NewAggregator(db).AggregateDay(ctx, start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("AggregateDay waited for occupied writer: %v", err)
	}
	if metrics.RequestCount != 1 || metrics.TotalTokens != 12 {
		t.Fatalf("unexpected reader metrics: %+v", metrics)
	}
}
