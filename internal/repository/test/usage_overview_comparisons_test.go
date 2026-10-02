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

func TestOverviewComparisonsShareRollupsAndExactBoundaries(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Date(2026, 9, 12, 12, 30, 0, 0, time.Local)
	start := end.Add(-4 * time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "outside", Timestamp: start.Add(-time.Minute), APIGroupKey: "key-a", Model: "model-a", TotalTokens: 9999},
		{EventKey: "left", Timestamp: start, APIGroupKey: "key-a", Model: "model-a", InputTokens: 100, CacheReadTokens: 40, OutputTokens: 20, TotalTokens: 120},
		{EventKey: "middle", Timestamp: start.Add(time.Hour), APIGroupKey: "key-b", Model: "model-a", InputTokens: 200, OutputTokens: 30, TotalTokens: 230},
		{EventKey: "failure", Timestamp: start.Add(2 * time.Hour), APIGroupKey: "key-b", Model: "model-b", Failed: true},
		{EventKey: "right", Timestamp: end.Add(-time.Minute), APIGroupKey: "key-a", Model: "model-b", InputTokens: 50, CacheCreationTokens: 10, OutputTokens: 40, TotalTokens: 90},
	}
	events = priceOverviewFixtureEvents(t, db, events)
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	filter := repodto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true}
	queries := captureOverviewDataQueries(t, db, "comparisons")
	overview, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Comparisons == nil {
		t.Fatal("missing comparisons")
	}
	a, b := overview.Comparisons.Models["model-a"], overview.Comparisons.APIKeys["key-b"]
	if a.Requests != 2 || a.TotalTokens != 350 || a.OutputTokens != 50 || a.CacheReadTokens != 40 || a.CostAvailable {
		t.Fatalf("model totals: %+v", a)
	}
	if b.Requests != 2 || b.Failures != 1 || b.TotalTokens != 230 {
		t.Fatalf("key totals: %+v", b)
	}
	if len(overview.Comparisons.Buckets) != 5 || overview.Comparisons.Granularity != "hourly" {
		t.Fatalf("expected five hourly buckets including idle hours: %+v", overview.Comparisons)
	}
	if a.TokenBuckets[overview.Comparisons.Buckets[0]] != 120 || a.TokenBuckets[overview.Comparisons.Buckets[1]] != 230 || overview.Comparisons.Models["model-b"].TokenBuckets[overview.Comparisons.Buckets[4]] != 90 {
		t.Fatal("boundary and rollup tokens must retain their original time buckets")
	}
	if overview.Usage.TotalRequests != 0 || len(overview.Series.Requests) != 0 {
		t.Fatal("comparison-only query must not build overview totals or series")
	}
	if len(*queries) != 6 {
		t.Fatalf("expected two boundary reads, one summary and three token series reads, got %d", len(*queries))
	}
	filter.APIGroupKey = "key-a"
	filtered, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Comparisons.APIKeys) != 1 || filtered.Comparisons.APIKeys["key-a"].TotalTokens != 210 || filtered.Usage.TotalTokens != 0 {
		t.Fatalf("key filter leaked: %+v", filtered.Comparisons)
	}
}

func TestOverviewComparisonsCustomDayNeverReadsRawEvents(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 365)
	zeroCost, unavailable := 0.0, int64(0)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 5, SuccessCount: 4, FailureCount: 1, TotalTokens: 100, CostUSD: &zeroCost, UnavailableCostCount: &unavailable},
		{BucketStart: end.AddDate(0, 0, -1), APIGroupKey: "key-b", Model: "model-a", RequestCount: 3, SuccessCount: 3, TotalTokens: 300, CostUSD: &zeroCost, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	queries := captureOverviewDataQueries(t, db, "comparisons_long")
	filter := repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true}
	overview, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Comparisons.Models["model-a"].Requests != 8 || len(overview.Comparisons.APIKeys) != 2 {
		t.Fatal("lost rollup dimensions")
	}
	if len(overview.Comparisons.Buckets) != 365 || overview.Comparisons.Granularity != "daily" {
		t.Fatal("custom day series must include empty days")
	}
	if got := overview.Comparisons.Models["model-a"].TokenBuckets; got[start.Format(time.DateOnly)] != 100 || got[end.AddDate(0, 0, -1).Format(time.DateOnly)] != 300 {
		t.Fatalf("daily rollup series lost tokens: %v", got)
	}
	assertOverviewQueryTables(t, *queries, false, true)
	if len(*queries) != 4 {
		t.Fatalf("expected one daily summary and three token series reads, got %d", len(*queries))
	}
	if !strings.Contains((*queries)[0], "api_group_key") {
		t.Fatal("missing comparison grouping")
	}
	if len(overview.Series.Requests) != 0 || overview.Usage.TotalRequests != 0 {
		t.Fatal("comparison-only projection must preserve comparisons without building the main series")
	}
	plain, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Comparisons != nil {
		t.Fatal("comparison collection must be opt-in for other overview callers")
	}
}

// 比较视图的完整桶与边界都读取落库金额，当前规则不能改写旧事件费用。
func TestOverviewComparisonUsesStoredCostInsteadOfCurrentRules(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Date(2026, 9, 12, 12, 30, 0, 0, time.Local)
	start := end.Add(-4 * time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "priced-left", Timestamp: start, APIGroupKey: "key-a", Model: "model-a", InputTokens: 1000000, TotalTokens: 1000000, CostUSD: overviewCostPtr(2.5), CostAvailable: overviewAvailabilityPtr(true)},
		{EventKey: "priced-middle", Timestamp: start.Add(time.Hour), APIGroupKey: "key-b", Model: "model-a", InputTokens: 1000000, CacheReadTokens: 100000, CacheCreationTokens: 50000, TotalTokens: 1000000, CostUSD: overviewCostPtr(4), CostAvailable: overviewAvailabilityPtr(true)},
	}
	events = priceOverviewFixtureEvents(t, db, events)
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	comparisonOnlyFilter := repodto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end, QueryNow: &end, ComparisonOnly: true}
	comparisonOnly, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, comparisonOnlyFilter, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !finiteStoredOverviewTestCost(comparisonOnly.Comparisons.APIKeys["key-a"].CostUSD, 2.5) || !finiteStoredOverviewTestCost(comparisonOnly.Comparisons.APIKeys["key-b"].CostUSD, 4) {
		t.Fatalf("comparison reread current price instead of stored event fees: key-a=%+v key-b=%+v", comparisonOnly.Comparisons.APIKeys["key-a"], comparisonOnly.Comparisons.APIKeys["key-b"])
	}
}
