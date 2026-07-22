package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type usageOverviewFiveDimensionRow struct {
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
	RequestCount        int64
	TotalTokens         int64
}

type usageOverviewFiveDimensionKey struct {
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
}

func TestUsageOverviewAggregationSeparatesFiveDimensionCombinations(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	db := openTestDatabase(t)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	alias := "gpt-alias"
	events := []entities.UsageEvent{
		{
			EventKey: "five-dimensions-1", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			Timestamp: now.Add(-50 * time.Minute), InputTokens: 10, OutputTokens: 2, TotalTokens: 12,
		},
		{
			EventKey: "five-dimensions-2", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "priority", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			Timestamp: now.Add(-40 * time.Minute), InputTokens: 20, OutputTokens: 3, TotalTokens: 23,
		},
		{
			EventKey: "five-dimensions-3", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "default", ResponseServiceTier: "priority", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			Timestamp: now.Add(-30 * time.Minute), InputTokens: 30, OutputTokens: 4, TotalTokens: 34,
		},
		{
			EventKey: "five-dimensions-4", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "max", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			Timestamp: now.Add(-20 * time.Minute), InputTokens: 40, OutputTokens: 5, TotalTokens: 45,
		},
		{
			EventKey: "five-dimensions-5", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "POST /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			Timestamp: now.Add(-10 * time.Minute), InputTokens: 50, OutputTokens: 6, TotalTokens: 56,
		},
		{
			EventKey: "five-dimensions-6", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexExecutor",
			Timestamp: now.Add(-5 * time.Minute), InputTokens: 60, OutputTokens: 7, TotalTokens: 67,
		},
		{
			EventKey: "five-dimensions-7", APIGroupKey: "api-a", Model: "gpt-a", ModelAlias: &alias, AuthIndex: "auth-a",
			ServiceTier: " default ", ResponseServiceTier: " default ", ReasoningEffort: " xhigh ", Endpoint: " GET /v1/responses ", ExecutorType: " CodexWebsocketsExecutor ",
			Timestamp: now.Add(-time.Minute), InputTokens: 70, OutputTokens: 8, TotalTokens: 78,
		},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert five-dimension events: %v", err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate five-dimension events: %v", err)
	}

	assertUsageOverviewFiveDimensionRows(t, db, "usage_overview_hourly_stats")
	assertUsageOverviewFiveDimensionRows(t, db, "usage_overview_daily_stats")

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load five-dimension checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 7 {
		t.Fatalf("expected checkpoint 7, got %d", checkpoint.LastAggregatedUsageEventID)
	}
}

func assertUsageOverviewFiveDimensionRows(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var count int64
	if err := db.Table(table).Where("api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ?", "api-a", "gpt-a", "auth-a", "gpt-alias").Count(&count).Error; err != nil {
		t.Fatalf("count %s five-dimension rows: %v", table, err)
	}
	if count != 6 {
		t.Fatalf("expected %s to contain 6 five-dimension rows, got %d", table, count)
	}

	var rows []usageOverviewFiveDimensionRow
	if err := db.Table(table).
		Select("service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, total_tokens").
		Where("api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ?", "api-a", "gpt-a", "auth-a", "gpt-alias").
		Find(&rows).Error; err != nil {
		t.Fatalf("load %s five-dimension rows: %v", table, err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 %s rows, got %d", table, len(rows))
	}
	want := map[usageOverviewFiveDimensionKey]struct {
		requestCount int64
		totalTokens  int64
	}{
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor"}:  {requestCount: 2, totalTokens: 90},
		{ServiceTier: "priority", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor"}: {requestCount: 1, totalTokens: 23},
		{ServiceTier: "default", ResponseServiceTier: "priority", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor"}: {requestCount: 1, totalTokens: 34},
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "max", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor"}:    {requestCount: 1, totalTokens: 45},
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "POST /v1/responses", ExecutorType: "CodexWebsocketsExecutor"}: {requestCount: 1, totalTokens: 56},
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexExecutor"}:            {requestCount: 1, totalTokens: 67},
	}
	for _, row := range rows {
		key := usageOverviewFiveDimensionKey{
			ServiceTier: row.ServiceTier, ResponseServiceTier: row.ResponseServiceTier, ReasoningEffort: row.ReasoningEffort,
			Endpoint: row.Endpoint, ExecutorType: row.ExecutorType,
		}
		expected, ok := want[key]
		if !ok {
			t.Fatalf("unexpected %s dimensions: %+v", table, row)
		}
		if row.RequestCount != expected.requestCount || row.TotalTokens != expected.totalTokens {
			t.Fatalf("unexpected %s totals for %+v: got requests=%d tokens=%d want requests=%d tokens=%d", table, key, row.RequestCount, row.TotalTokens, expected.requestCount, expected.totalTokens)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing %s dimension rows: %+v", table, want)
	}
}
