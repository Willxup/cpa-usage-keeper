package repository

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
)

func TestListUsageEventsWithFilterAppliesTimeBoundsAndPagination(t *testing.T) {
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
	page, err := ListUsageEventsWithFilter(db, UsageQueryFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 2 || page.TotalPages != 2 || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("unexpected pagination metadata: %+v", page)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one row after page size, got %d", len(page.Events))
	}
	if page.Events[0].Source != "source-c" {
		t.Fatalf("expected newest in-range row first, got %+v", page.Events[0])
	}
}

func TestListUsageEventsWithFilterPagesByTimestampAndID(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-pages.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp, Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp, Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: timestamp.Add(-time.Hour), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	firstPage, err := ListUsageEventsWithFilter(db, UsageQueryFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	secondPage, err := ListUsageEventsWithFilter(db, UsageQueryFilter{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if firstPage.TotalCount != 3 || firstPage.TotalPages != 3 || secondPage.TotalCount != 3 || secondPage.TotalPages != 3 {
		t.Fatalf("unexpected page metadata: first=%+v second=%+v", firstPage, secondPage)
	}
	if len(firstPage.Events) != 1 || len(secondPage.Events) != 1 {
		t.Fatalf("expected one event on each page: first=%+v second=%+v", firstPage, secondPage)
	}
	if firstPage.Events[0].ID <= secondPage.Events[0].ID {
		t.Fatalf("expected id desc tie-breaker, first=%+v second=%+v", firstPage.Events[0], secondPage.Events[0])
	}
}

func TestListUsageEventsWithFilterAppliesModelSourceAndResultFilters(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-filtered.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-a", Failed: true, TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 30},
		{EventKey: "event-4", SnapshotRunID: 1, APIGroupKey: "provider-c", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Source: "source-b", Failed: false, TotalTokens: 40},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, UsageQueryFilter{Page: 1, PageSize: 20, Model: "claude-sonnet", Source: "source-a", Result: "success"})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 1 || len(page.Events) != 1 {
		t.Fatalf("expected one matching event, got %+v", page)
	}
	if page.Events[0].Model != "claude-sonnet" || page.Events[0].Source != "source-a" || page.Events[0].Failed {
		t.Fatalf("unexpected filtered event: %+v", page.Events[0])
	}
}

func TestListUsageEventFilterOptionsWithFilterReturnsStableOptions(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-events-facets.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", Failed: true, TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "gpt-5", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-a", Failed: false, TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	options, err := ListUsageEventFilterOptionsWithFilter(db, UsageQueryFilter{Result: "success"})
	if err != nil {
		t.Fatalf("ListUsageEventFilterOptionsWithFilter returned error: %v", err)
	}
	if len(options.Models) != 2 || options.Models[0] != "claude-sonnet" || options.Models[1] != "gpt-5" {
		t.Fatalf("expected stable model options, got %+v", options.Models)
	}
	if len(options.Sources) != 2 || options.Sources[0] != "source-a" || options.Sources[1] != "source-b" {
		t.Fatalf("expected stable source options, got %+v", options.Sources)
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
