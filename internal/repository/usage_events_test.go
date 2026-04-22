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

func TestListUsageAnalysisWithFilterAggregatesApisAndModels(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-analysis.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{
			EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Failed: false, LatencyMS: 100,
			InputTokens: 10, OutputTokens: 4, ReasoningTokens: 2, CachedTokens: 1, TotalTokens: 17,
		},
		{
			EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Failed: true, LatencyMS: 250,
			InputTokens: 20, OutputTokens: 5, ReasoningTokens: 0, CachedTokens: 0, TotalTokens: 25,
		},
		{
			EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "gpt-5",
			Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Failed: false, LatencyMS: 400,
			InputTokens: 30, OutputTokens: 7, ReasoningTokens: 3, CachedTokens: 2, TotalTokens: 42,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 11, 30, 0, 0, time.UTC)
	apiRows, modelRows, err := ListUsageAnalysisWithFilter(db, UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("ListUsageAnalysisWithFilter returned error: %v", err)
	}
	if len(apiRows) != 2 {
		t.Fatalf("expected two api rows, got %d", len(apiRows))
	}
	if len(modelRows) != 2 {
		t.Fatalf("expected two model rows, got %d", len(modelRows))
	}
	if apiRows[0].APIGroupKey != "provider-a" || apiRows[0].TotalRequests != 1 || apiRows[0].FailureCount != 1 || apiRows[0].TotalTokens != 25 {
		t.Fatalf("unexpected first api row: %+v", apiRows[0])
	}
	modelByName := map[string]UsageAnalysisModelStatRecord{}
	for _, row := range modelRows {
		modelByName[row.Model] = row
	}
	if row := modelByName["gpt-5"]; row.Model != "gpt-5" || row.TotalRequests != 1 || row.TotalLatencyMS != 400 || row.LatencySampleCount != 1 {
		t.Fatalf("unexpected gpt-5 model row: %+v", row)
	}
	if row := modelByName["claude-sonnet"]; row.Model != "claude-sonnet" || row.FailureCount != 1 || row.InputTokens != 20 || row.CachedTokens != 0 {
		t.Fatalf("unexpected claude-sonnet model row: %+v", row)
	}
}

