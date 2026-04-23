package repository

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
)

func TestBuildUsageSnapshotWithFilterAppliesTimeBounds(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC)
	snapshot, err := BuildUsageSnapshotWithFilter(db, UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageSnapshotWithFilter returned error: %v", err)
	}

	if snapshot.TotalRequests != 1 {
		t.Fatalf("expected one in-range request, got %+v", snapshot)
	}
	if snapshot.TotalTokens != 20 {
		t.Fatalf("expected in-range tokens only, got %+v", snapshot)
	}
	if len(snapshot.APIs) != 1 {
		t.Fatalf("expected one API in filtered snapshot, got %+v", snapshot.APIs)
	}
	if snapshot.RequestsByHour["2026-04-16T10:00:00Z"] != 1 {
		t.Fatalf("expected only 10:00 bucket to remain, got %+v", snapshot.RequestsByHour)
	}
	if _, ok := snapshot.RequestsByHour["2026-04-16T09:00:00Z"]; ok {
		t.Fatalf("expected 09:00 bucket to be filtered out, got %+v", snapshot.RequestsByHour)
	}
}
