package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
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
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	filter := repodto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true}
	queries := captureOverviewDataQueries(t, db, "comparisons")
	overview, err := repository.BuildUsageOverviewWithFilter(db, filter, emptyPricingResolverForTest())
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
	if overview.Usage.TotalRequests != 0 || len(overview.Series.Requests) != 0 {
		t.Fatal("comparison-only query must not build overview totals or series")
	}
	if len(*queries) != 3 {
		t.Fatalf("expected two boundary reads and one rollup read, got %d", len(*queries))
	}
	filter.APIGroupKey = "key-a"
	filtered, err := repository.BuildUsageOverviewWithFilter(db, filter, emptyPricingResolverForTest())
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
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 5, SuccessCount: 4, FailureCount: 1, TotalTokens: 100},
		{BucketStart: end.AddDate(0, 0, -1), APIGroupKey: "key-b", Model: "model-a", RequestCount: 3, SuccessCount: 3, TotalTokens: 300},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	queries := captureOverviewDataQueries(t, db, "comparisons_long")
	filter := repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true}
	overview, err := repository.BuildUsageOverviewWithFilter(db, filter, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Comparisons.Models["model-a"].Requests != 8 || len(overview.Comparisons.APIKeys) != 2 {
		t.Fatal("lost rollup dimensions")
	}
	assertOverviewQueryTables(t, *queries, false, true)
	if len(*queries) != 1 {
		t.Fatalf("expected one daily read, got %d", len(*queries))
	}
	if !strings.Contains((*queries)[0], "api_group_key") {
		t.Fatal("missing comparison grouping")
	}
	if len(overview.Series.Requests) != 0 || overview.Usage.TotalRequests != 0 {
		t.Fatal("comparison-only projection must preserve comparisons without building the main series")
	}
	plain, err := repository.BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	if plain.Comparisons != nil {
		t.Fatal("comparison collection must be opt-in for other overview callers")
	}
}

func TestOverviewComparisonCostUsesTheSamePricingRules(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Date(2026, 9, 12, 12, 30, 0, 0, time.Local)
	start := end.Add(-4 * time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "priced-left", Timestamp: start, APIGroupKey: "key-a", Model: "model-a", InputTokens: 1000000, TotalTokens: 1000000},
		{EventKey: "priced-middle", Timestamp: start.Add(time.Hour), APIGroupKey: "key-b", Model: "model-a", InputTokens: 1000000, CacheReadTokens: 100000, CacheCreationTokens: 50000, TotalTokens: 1000000},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "api_group_key", Value: "key-b", Multiplier: 2}})
	comparisonOnlyFilter := repodto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end, QueryNow: &end, ComparisonOnly: true}
	comparisonOnly, err := repository.BuildUsageOverviewWithFilter(db, comparisonOnlyFilter, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(comparisonOnly.Comparisons.APIKeys["key-b"].CostUSD-1.7) > 1e-9 {
		t.Fatalf("comparison-only cache pricing diverged: got=%v", comparisonOnly.Comparisons.APIKeys["key-b"].CostUSD)
	}
}
