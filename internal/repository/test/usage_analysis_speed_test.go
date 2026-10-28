package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestAnalysisModelSpeedUsesRollupsAndWeightedGroups(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 10, 8, 0, 0, 0, 0, time.Local)
	end := start.Add(24 * time.Hour)
	keys := []entities.CPAAPIKey{{APIKey: "speed-key"}, {APIKey: "other-key"}, {APIKey: "deleted-key", IsDeleted: true}}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	events := []entities.UsageEvent{
		{EventKey: "a-fast", APIGroupKey: "speed-key", Model: "a", Timestamp: start, OutputTokens: 100, LatencyMS: 1000, TotalTokens: 100},
		{EventKey: "a-slow", APIGroupKey: "speed-key", Model: "a", Timestamp: start.Add(time.Hour), OutputTokens: 100, LatencyMS: 10000, TotalTokens: 100, Failed: true},
		{EventKey: "a-slow-2", APIGroupKey: "speed-key", Model: "a", Timestamp: start.Add(time.Hour), OutputTokens: 100, LatencyMS: 10000, TotalTokens: 100},
		{EventKey: "b-invalid", APIGroupKey: "speed-key", Model: "b", Timestamp: start, OutputTokens: 1000, TotalTokens: 1000},
		{EventKey: "c-invalid", APIGroupKey: "speed-key", Model: "c", Timestamp: start, LatencyMS: 1000, TotalTokens: 1000},
		{EventKey: "other", APIGroupKey: "other-key", Model: "other", Timestamp: start, OutputTokens: 999, LatencyMS: 1, TotalTokens: 999},
		{EventKey: "deleted", APIGroupKey: "deleted-key", Model: "deleted", Timestamp: start, OutputTokens: 9999, LatencyMS: 1, TotalTokens: 9999},
	}
	for i := range events {
		events[i].CostUSD = new(float64(0))
		events[i].CostAvailable = new(true)
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatal(err)
	}
	queries := captureAllAnalysisQueries(t, db)
	result, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, APIGroupKey: "speed-key"})
	if err != nil {
		t.Fatal(err)
	}
	speed := result.ModelSpeed
	if speed.RangeStart == nil || !speed.RangeStart.Equal(start) || speed.RangeEnd == nil || !speed.RangeEnd.Equal(end) || len(speed.Buckets) != 8 || len(speed.Series) != 3 {
		t.Fatalf("unexpected speed: %+v", speed)
	}
	if !speed.Buckets[0].Start.Equal(start) || !speed.Buckets[0].End.Equal(start.Add(3*time.Hour)) {
		t.Fatalf("bucket bounds: %+v", speed.Buckets)
	}
	if speed.Series[0].Model != "b" || speed.Series[1].Model != "c" || speed.Series[2].Model != "a" {
		t.Fatalf("Token order: %+v", speed.Series)
	}
	a := speed.Series[2]
	if a.TotalTokens != 300 || a.SpeedSampleCount != 3 || len(a.Samples) != 8 || a.Samples[0].SpeedSampleCount != 3 || a.Samples[0].MeanTPS == nil {
		t.Fatalf("samples: %+v", a)
	}
	assertFloatClose(t, a.Samples[0].SpeedTPSSum, 120)
	assertFloatClose(t, *a.Samples[0].MeanTPS, 40)
	if a.Samples[1].MeanTPS != nil || speed.Series[0].Samples[0].MeanTPS != nil {
		t.Fatal("empty samples must have null means")
	}
	for _, query := range *queries {
		if strings.Contains(query, "usage_events") || strings.Contains(query, "usage_latency_stats") {
			t.Fatalf("raw/latency query: %s", query)
		}
	}
	requireSingleAnalysisRollupQuery(t, *queries, "usage_overview_hourly_stats")
}

func TestAnalysisModelSpeedDailyGroupsFollowDSTBoundaries(t *testing.T) {
	previous := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	db := openTestDatabase(t)
	start := time.Date(2026, 3, 7, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 9)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "dst-key"}).Error; err != nil {
		t.Fatal(err)
	}
	cost, count := 0.0, int64(0)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: start, APIGroupKey: "dst-key", Model: "a", SpeedTPSSum: 100, SpeedSampleCount: 1, TotalTokens: 1, CostUSD: &cost, UnavailableCostCount: &count},
		{BucketStart: start.AddDate(0, 0, 1), APIGroupKey: "dst-key", Model: "a", SpeedTPSSum: 20, SpeedSampleCount: 2, TotalTokens: 2, CostUSD: &cost, UnavailableCostCount: &count},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	result, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	speed := result.ModelSpeed
	if len(speed.Buckets) != 8 || len(speed.Series) != 1 {
		t.Fatalf("unexpected speed: %+v", speed)
	}
	if !speed.Buckets[0].Start.Equal(start) || !speed.Buckets[0].End.Equal(start.AddDate(0, 0, 2)) || speed.Buckets[0].End.Sub(start) != 47*time.Hour {
		t.Fatalf("must merge natural days: %+v", speed.Buckets[0])
	}
	assertFloatClose(t, *speed.Series[0].Samples[0].MeanTPS, 40)
	for i := 1; i < len(speed.Buckets); i++ {
		if !speed.Buckets[i].Start.Equal(speed.Buckets[i-1].End) {
			t.Fatal("gap between groups")
		}
	}
	if !speed.Buckets[7].End.Equal(end) {
		t.Fatal("wrong final boundary")
	}
}

func TestAnalysisModelSpeedDailyGroupsAcrossMidnightDST(t *testing.T) {
	location, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	db := openTestDatabase(t)
	start := time.Date(2026, 9, 4, 0, 0, 0, 0, location)
	end := time.Date(2026, 9, 9, 0, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "midnight-dst-key"}).Error; err != nil {
		t.Fatal(err)
	}
	cost, unavailable := 0.0, int64(0)
	for day := 4; day < 9; day++ {
		// 与正式日聚合一致，逐个事件日期独立生成午夜，而非沿用前一边界钟点。
		row := entities.UsageOverviewDailyStat{BucketStart: time.Date(2026, 9, day, 0, 0, 0, 0, location), APIGroupKey: "midnight-dst-key", Model: "a", SpeedTPSSum: float64(day), SpeedSampleCount: 1, TotalTokens: 1, CostUSD: &cost, UnavailableCostCount: &unavailable}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	speed := result.ModelSpeed
	if len(speed.Buckets) != 5 || len(speed.Series) != 1 {
		t.Fatalf("wrong calendar grouping: %+v", speed)
	}
	for i, bucket := range speed.Buckets {
		// 9月6日没有00:00；只修复读取后的业务区间，旧存储键仍由上面的fixture保留。
		startHour, endHour := 0, 0
		if 4+i == 6 {
			startHour = 1
		}
		if 5+i == 6 {
			endHour = 1
		}
		wantStart := time.Date(2026, 9, 4+i, startHour, 0, 0, 0, location)
		wantEnd := time.Date(2026, 9, 5+i, endHour, 0, 0, 0, location)
		if !bucket.Start.Equal(wantStart) || !bucket.End.Equal(wantEnd) {
			t.Fatalf("bucket %d: %+v, want %s..%s", i, bucket, wantStart, wantEnd)
		}
		if speed.Series[0].Samples[i].SpeedSampleCount != 1 {
			t.Fatalf("wrong sample assignment at %d", i)
		}
		assertFloatClose(t, *speed.Series[0].Samples[i].MeanTPS, float64(i+4))
	}
}
