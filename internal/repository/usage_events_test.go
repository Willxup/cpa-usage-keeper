package repository

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
)

func TestListUsageEventsWithFilterAppliesTimeBoundsAndLimit(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	rows, err := ListUsageEventsWithFilter(db, UsageQueryFilter{StartTime: &start, EndTime: &end, Limit: 1})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row after limit, got %d", len(rows))
	}
	if rows[0].Source != "source-c" {
		t.Fatalf("expected newest in-range row first, got %+v", rows[0])
	}
}

