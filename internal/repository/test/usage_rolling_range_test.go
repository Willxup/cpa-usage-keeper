package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildAnalysisIncludesCurrentHourStatsForArbitraryHourRange(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-rolling-hour.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 5, 20, 20, 14, 21, 0, location)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, location)
	currentHour := time.Date(2026, 5, 21, 9, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: currentHour, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
		RequestCount: 6, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert current hour stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "13h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(currentHour) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("expected arbitrary hour range to include current hour stats, got %+v", analysis.TokenUsage)
	}
}

func TestBuildAnalysisIncludesCurrentHourStatsForRollingDayRange(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-rolling-day.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, location)
	start := end.Add(-13 * 24 * time.Hour)
	currentHour := time.Date(2026, 5, 21, 9, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: currentHour, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
		RequestCount: 6, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert current hour stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "13d", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	currentDay := time.Date(2026, 5, 21, 0, 0, 0, 0, location)
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(currentDay) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("expected rolling day range to include current hour stats, got %+v", analysis.TokenUsage)
	}
}

func TestBuildAnalysisKeepsDailyStatsAcrossDSTBoundary(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-dst-day.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 3, 8, 9, 14, 0, 0, location)
	end := time.Date(2026, 3, 16, 9, 14, 0, 0, location)
	dailyBucket := time.Date(2026, 3, 9, 0, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: dailyBucket, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
		RequestCount: 6, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert daily stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "8d", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(dailyBucket) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("expected DST-adjacent daily stat to remain included, got %+v", analysis.TokenUsage)
	}
}

func TestBuildUsageOverviewUsesShortHealthWindowForArbitraryHourRange(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "overview-rolling-hour.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, time.Local)
	start := end.Add(-13 * time.Hour)

	overview, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{
		Range: "13h", StartTime: &start, EndTime: &end, QueryNow: &end,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	expectedStart := end.Add(-24 * time.Hour)
	if !overview.Health.WindowStart.Equal(expectedStart) || !overview.Health.WindowEnd.Equal(end) {
		t.Fatalf("expected arbitrary hour range health window %s - %s, got %s - %s", expectedStart, end, overview.Health.WindowStart, overview.Health.WindowEnd)
	}
}
