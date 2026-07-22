package test

import (
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func TestCustomHourOverviewReadsCompleteHourlyBucketsWithoutUsageEvents(t *testing.T) {
	db := openTestDatabase(t)
	queryNow := time.Date(2026, 7, 22, 10, 30, 0, 0, time.Local)
	start := time.Date(2026, 7, 22, 6, 0, 0, 0, time.Local)
	end := time.Date(2026, 7, 22, 11, 0, 0, 0, time.Local)
	rows := make([]entities.UsageOverviewHourlyStat, 0, 5)
	for bucket := start; bucket.Before(end); bucket = bucket.Add(time.Hour) {
		rows = append(rows, entities.UsageOverviewHourlyStat{
			BucketStart: bucket, APIGroupKey: "provider-a", Model: "model-a",
			RequestCount: 1, SuccessCount: 1, InputTokens: 10, TotalTokens: 10,
		})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed hourly rollups: %v", err)
	}

	queries := captureOverviewDataQueries(t, db, "custom_hour")
	overview, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &queryNow,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 5 || overview.Usage.TotalTokens != 50 {
		t.Fatalf("expected all five complete hourly buckets, got %+v", overview.Usage)
	}
	assertOverviewQueryTables(t, *queries, true, false)
}

func TestCustomOverviewRollupQueryProjectsAndGroupsOnlyCardDimensions(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 22, 6, 0, 0, 0, time.Local)
	end := start.Add(5 * time.Hour)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: start, APIGroupKey: "provider-a", Model: "model-a", AuthIndex: "auth-a",
		RequestCount: 1, SuccessCount: 1, InputTokens: 10, TotalTokens: 10,
	}).Error; err != nil {
		t.Fatalf("seed hourly rollup: %v", err)
	}

	queries := captureOverviewDataQueries(t, db, "projection")
	if _, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end,
	}); err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	query := findOverviewRollupQuery(t, *queries, "usage_overview_hourly_stats")
	for _, unwanted := range []string{"`id`", "auth_index", "cached_tokens", "created_at", "updated_at", "select *"} {
		if strings.Contains(query, unwanted) {
			t.Fatalf("rollup projection should exclude %q:\n%s", unwanted, query)
		}
	}
	if !strings.Contains(query, "group by") || !strings.Contains(query, "bucket_start") || !strings.Contains(query, "model_alias") {
		t.Fatalf("expected rollup query to group by bucket and pricing dimensions:\n%s", query)
	}
}

func TestCustomOverviewGroupedProjectionPreservesPerRowCostNormalization(t *testing.T) {
	db := openTestDatabase(t)
	if _, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{
		Model: "model-a", PromptPricePer1M: 1, CompletionPricePer1M: 0, CacheReadPricePer1M: 0,
	}); err != nil {
		t.Fatalf("seed model price: %v", err)
	}
	start := time.Date(2026, 7, 22, 6, 0, 0, 0, time.Local)
	end := start.Add(5 * time.Hour)
	rows := []entities.UsageOverviewHourlyStat{
		{
			BucketStart: start, APIGroupKey: "provider-a", Model: "model-a", AuthIndex: "auth-a",
			RequestCount: 1, SuccessCount: 1, InputTokens: 10, CacheReadTokens: 20, TotalTokens: 30,
		},
		{
			BucketStart: start, APIGroupKey: "provider-a", Model: "model-a", AuthIndex: "auth-b",
			RequestCount: 1, SuccessCount: 1, InputTokens: 20, TotalTokens: 20,
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed adversarial hourly rollups: %v", err)
	}

	overview, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	wantCost := 20.0 / 1_000_000.0
	if math.Abs(overview.Summary.TotalCost-wantCost) > 0.000000001 {
		t.Fatalf("expected per-row normalized cost %.12f, got %.12f", wantCost, overview.Summary.TotalCost)
	}
	if overview.Usage.TotalRequests != 2 || overview.Summary.InputTokens != 30 || overview.Summary.CacheReadTokens != 20 {
		t.Fatalf("expected grouped display totals to preserve raw sums, usage=%+v summary=%+v", overview.Usage, overview.Summary)
	}
	bucket := timeutil.FormatStorageTime(start)
	if overview.Series.Requests[bucket] != 2 || overview.Series.CacheReadRate[bucket] == nil || math.Abs(*overview.Series.CacheReadRate[bucket]-200.0/3.0) > 0.000000001 {
		t.Fatalf("expected grouped series parity for %s, got %+v", bucket, overview.Series)
	}
}

func TestPresetOverviewRawBoundaryUsesCardOnlyProjection(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Date(2026, 7, 22, 10, 30, 0, 0, time.Local)
	start := end.Add(-4 * time.Hour)
	queries := captureOverviewDataQueries(t, db, "raw_projection")

	if _, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "4h", StartTime: &start, EndTime: &end, QueryNow: &end,
	}); err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	query := findOverviewRollupQuery(t, *queries, "usage_events")
	for _, unwanted := range []string{"provider", "auth_type", "source", "auth_index", "generate", "latency_ms", "ttft_ms"} {
		if strings.Contains(query, unwanted) {
			t.Fatalf("Overview boundary projection should exclude %q:\n%s", unwanted, query)
		}
	}
	for _, required := range []string{"api_group_key", "model", "model_alias", "timestamp", "failed", "input_tokens", "total_tokens"} {
		if !strings.Contains(query, required) {
			t.Fatalf("Overview boundary projection should include %q:\n%s", required, query)
		}
	}
}

func TestCustomDayOverviewReadsCompleteDailyBucketsWithoutUsageEvents(t *testing.T) {
	db := openTestDatabase(t)
	queryNow := time.Date(2026, 7, 22, 10, 30, 0, 0, time.Local)
	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 7, 23, 0, 0, 0, 0, time.Local)
	rows := make([]entities.UsageOverviewDailyStat, 0, 3)
	for bucket := start; bucket.Before(end); bucket = bucket.AddDate(0, 0, 1) {
		rows = append(rows, entities.UsageOverviewDailyStat{
			BucketStart: bucket, APIGroupKey: "provider-a", Model: "model-a",
			RequestCount: 1, SuccessCount: 1, InputTokens: 10, TotalTokens: 10,
		})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed daily rollups: %v", err)
	}

	queries := captureOverviewDataQueries(t, db, "custom_day")
	overview, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &queryNow,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 3 || overview.Usage.TotalTokens != 30 {
		t.Fatalf("expected all three complete daily buckets, got %+v", overview.Usage)
	}
	assertOverviewQueryTables(t, *queries, false, true)
}

func captureOverviewDataQueries(t *testing.T, db *gorm.DB, suffix string) *[]string {
	t.Helper()
	queries := make([]string, 0, 3)
	callbackName := "test:capture_overview_data_queries_" + suffix
	capture := func(tx *gorm.DB) {
		queries = append(queries, strings.ToLower(tx.Statement.SQL.String()))
	}
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, capture); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, capture); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
		_ = db.Callback().Row().Remove(callbackName)
	})
	return &queries
}

func assertOverviewQueryTables(t *testing.T, queries []string, wantHourly, wantDaily bool) {
	t.Helper()
	joined := strings.Join(queries, "\n")
	if strings.Contains(joined, "usage_events") {
		t.Fatalf("Custom Overview must not query usage_events:\n%s", joined)
	}
	if got := strings.Contains(joined, "usage_overview_hourly_stats"); got != wantHourly {
		t.Fatalf("hourly rollup query presence=%t, want %t:\n%s", got, wantHourly, joined)
	}
	if got := strings.Contains(joined, "usage_overview_daily_stats"); got != wantDaily {
		t.Fatalf("daily rollup query presence=%t, want %t:\n%s", got, wantDaily, joined)
	}
}

func findOverviewRollupQuery(t *testing.T, queries []string, table string) string {
	t.Helper()
	for _, query := range queries {
		if strings.Contains(query, table) {
			return query
		}
	}
	t.Fatalf("expected query for %s, got:\n%s", table, strings.Join(queries, "\n"))
	return ""
}
