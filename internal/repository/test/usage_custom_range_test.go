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

func TestListUsageEventsExcludesCustomRangeEndBoundary(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "custom-events.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)
	end := time.Date(2026, 7, 17, 16, 0, 0, 0, time.Local)
	events := []entities.UsageEvent{
		{EventKey: "at-start", Model: "gpt-5", Timestamp: start, TotalTokens: 10},
		{EventKey: "before-end", Model: "gpt-5", Timestamp: end.Add(-time.Nanosecond), TotalTokens: 20},
		{EventKey: "at-end", Model: "gpt-5", Timestamp: end, TotalTokens: 30},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := repository.ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
		Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 2 || len(page.Events) != 2 {
		t.Fatalf("expected custom range to exclude event at end boundary, got %+v", page)
	}
	for _, event := range page.Events {
		if event.Timestamp.Equal(end) {
			t.Fatalf("expected exclusive end boundary, got %+v", event)
		}
	}
}

func TestBuildAnalysisUsesCustomHourRollupsWithoutUsageEvents(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "custom-analysis.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)
	end := time.Date(2026, 7, 17, 16, 0, 0, 0, time.Local)
	selectedEndHour := end.Add(-time.Hour)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-custom", DisplayKey: "sk-*********custom"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: selectedEndHour, APIGroupKey: "sk-custom", Model: "gpt-5",
		RequestCount: 2, InputTokens: 70, OutputTokens: 30, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert hourly stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(selectedEndHour) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("expected custom analysis to use selected hourly rollup, got %+v", analysis.TokenUsage)
	}
}
