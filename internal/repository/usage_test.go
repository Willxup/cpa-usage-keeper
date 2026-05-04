package repository

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func TestBuildUsageSnapshotReturnsEmptyStructureWithoutEvents(t *testing.T) {
	db := openUsageTestDatabase(t)

	snapshot, err := BuildUsageSnapshot(db)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot returned error: %v", err)
	}
	if snapshot.TotalRequests != 0 || snapshot.TotalTokens != 0 || snapshot.SuccessCount != 0 || snapshot.FailureCount != 0 {
		t.Fatalf("expected empty totals, got %+v", snapshot)
	}
	if len(snapshot.APIs) != 0 || len(snapshot.RequestsByDay) != 0 || len(snapshot.RequestsByHour) != 0 {
		t.Fatalf("expected empty aggregates, got %+v", snapshot)
	}
}

func TestBuildUsageSnapshotAggregatesEvents(t *testing.T) {
	db := openUsageTestDatabase(t)
	events := []models.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "codex-a", AuthIndex: "1", Failed: false, LatencyMS: 100, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 0, TotalTokens: 35},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "codex-b", AuthIndex: "2", Failed: true, LatencyMS: 200, InputTokens: 2, OutputTokens: 3, ReasoningTokens: 0, CachedTokens: 0, TotalTokens: 5},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), Source: "codex-c", AuthIndex: "3", Failed: false, LatencyMS: 300, InputTokens: 100, OutputTokens: 50, ReasoningTokens: 25, CachedTokens: 10, TotalTokens: 185},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	snapshot, err := BuildUsageSnapshot(db)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot returned error: %v", err)
	}
	if snapshot.TotalRequests != 3 || snapshot.SuccessCount != 2 || snapshot.FailureCount != 1 || snapshot.TotalTokens != 225 {
		t.Fatalf("unexpected totals: %+v", snapshot)
	}
	if snapshot.RequestsByDay["2026-04-16"] != 2 || snapshot.RequestsByDay["2026-04-17"] != 1 {
		t.Fatalf("unexpected requests by day: %+v", snapshot.RequestsByDay)
	}
	if snapshot.TokensByHour["2026-04-16T09:00:00Z"] != 35 || snapshot.TokensByHour["2026-04-17T10:00:00Z"] != 185 {
		t.Fatalf("unexpected tokens by hour: %+v", snapshot.TokensByHour)
	}
	providerA := snapshot.APIs["provider-a"]
	if providerA.TotalRequests != 2 || providerA.TotalTokens != 40 {
		t.Fatalf("unexpected provider-a stats: %+v", providerA)
	}
	model := providerA.Models["claude-sonnet"]
	if model.TotalRequests != 2 || model.TotalTokens != 40 || len(model.Details) != 2 {
		t.Fatalf("unexpected model stats: %+v", model)
	}
	if !model.Details[0].Timestamp.Before(model.Details[1].Timestamp) {
		t.Fatalf("expected details to be sorted ascending, got %+v", model.Details)
	}
}

func TestBuildUsageSnapshotBucketsDaysByLocalTime(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openUsageTestDatabase(t)
	events := []models.UsageEvent{{
		EventKey:      "event-local-day",
		APIGroupKey:   "provider-a",
		Model:         "claude-sonnet",
		Timestamp:     time.Date(2026, 4, 16, 23, 30, 0, 0, time.UTC),
		TotalTokens:   20,
	}}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	snapshot, err := BuildUsageSnapshot(db)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot returned error: %v", err)
	}
	if snapshot.RequestsByDay["2026-04-17"] != 1 {
		t.Fatalf("expected event to be bucketed by local day, got %+v", snapshot.RequestsByDay)
	}
}

func TestUsageOverviewDailyBucketUsesLocalTime(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	bucketKey, bucketMinutes := usageOverviewBucket(time.Date(2026, 4, 16, 23, 30, 0, 0, time.UTC), true)

	if bucketKey != "2026-04-17" || bucketMinutes != 24*60 {
		t.Fatalf("expected local day bucket 2026-04-17/1440, got %s/%d", bucketKey, bucketMinutes)
	}
}

func TestBuildUsageSnapshotPreservesStoredAPIKey(t *testing.T) {
	db := openUsageTestDatabase(t)
	events := []models.UsageEvent{{
		EventKey:      "event-1",
		APIGroupKey:   "sk-live-secret-value",
		Model:         "claude-sonnet",
		Timestamp:     time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
		Source:        "source-a",
		AuthIndex:     "1",
		TotalTokens:   20,
	}}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	snapshot, err := BuildUsageSnapshot(db)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot returned error: %v", err)
	}
	if _, ok := snapshot.APIs["sk-live-secret-value"]; !ok {
		t.Fatalf("expected repository snapshot to preserve stored API key")
	}
}

func TestUsageAggregatesApplyModelSourceAuthAndResultFilters(t *testing.T) {
	db := openUsageTestDatabase(t)
	events := []models.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", Failed: false, TotalTokens: 35},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", Failed: true, TotalTokens: 5},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", Failed: false, TotalTokens: 185},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	filter := UsageQueryFilter{Model: "claude-sonnet", Source: "source-a", AuthIndex: "1", Result: "success"}

	snapshot, err := BuildUsageSnapshotWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageSnapshotWithFilter returned error: %v", err)
	}
	if snapshot.TotalRequests != 1 || snapshot.SuccessCount != 1 || snapshot.FailureCount != 0 || snapshot.TotalTokens != 35 {
		t.Fatalf("expected snapshot to include only matching successful event, got %+v", snapshot)
	}

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	if overview.Summary.RequestCount != 1 || overview.Summary.TokenCount != 35 {
		t.Fatalf("expected overview to include only matching successful event, got %+v", overview.Summary)
	}

	credentials, err := ListUsageCredentialStatsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("ListUsageCredentialStatsWithFilter returned error: %v", err)
	}
	if len(credentials) != 1 || credentials[0].Source != "source-a" || credentials[0].AuthIndex != "1" || credentials[0].Failed || credentials[0].RequestCount != 1 {
		t.Fatalf("expected credential stats to include only matching successful event, got %+v", credentials)
	}

	apis, models, err := ListUsageAnalysisWithFilter(db, filter)
	if err != nil {
		t.Fatalf("ListUsageAnalysisWithFilter returned error: %v", err)
	}
	if len(apis) != 1 || apis[0].APIGroupKey != "provider-a" || apis[0].TotalRequests != 1 || apis[0].FailureCount != 0 {
		t.Fatalf("expected analysis API stats to include only matching successful event, got %+v", apis)
	}
	if len(models) != 1 || models[0].Model != "claude-sonnet" || models[0].TotalRequests != 1 || models[0].FailureCount != 0 {
		t.Fatalf("expected analysis model stats to include only matching successful event, got %+v", models)
	}
}

func openUsageTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
