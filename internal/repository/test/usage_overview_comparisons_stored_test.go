package test

import (
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

// 比较视图从相同边界事件读费用，缓存和数据库回退的四维结果一致。
func TestOverviewComparisonBoundaryStoredFeesMatchCacheAndDatabase(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	end := time.Date(2026, 9, 12, 12, 30, 0, 0, time.UTC)
	queryNow := end.Add(10 * time.Minute)
	start := end.Add(-30 * time.Minute)
	aliasA, aliasB := "old-alias-a", "old-alias-b"
	events := []entities.UsageEvent{
		{EventKey: "auth-file", Timestamp: start.Add(5 * time.Minute), APIGroupKey: "deleted-key", Model: "model-a", ModelAlias: &aliasA, AuthType: "oauth", Source: "file-source", AuthIndex: "auth-1", InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: overviewCostPtr(2.5), CostAvailable: overviewAvailabilityPtr(true)},
		{EventKey: "provider", Timestamp: start.Add(10 * time.Minute), APIGroupKey: "deleted-key", Model: "model-a", ModelAlias: &aliasB, AuthType: "apikey", Source: "provider-source", AuthIndex: "provider-1", InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: overviewCostPtr(3.75), CostAvailable: overviewAvailabilityPtr(true)},
		{EventKey: "unknown", Timestamp: start.Add(15 * time.Minute), APIGroupKey: "other-key", Model: "model-b", AuthIndex: "unknown-identity", InputTokens: 10, TotalTokens: 10, CostUSD: overviewCostPtr(0), CostAvailable: overviewAvailabilityPtr(false)},
		{EventKey: "outside-end", Timestamp: end.Add(time.Minute), APIGroupKey: "deleted-key", Model: "model-a", InputTokens: 10, TotalTokens: 10, CostUSD: overviewCostPtr(99), CostAvailable: overviewAvailabilityPtr(true)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed comparison events: %v", err)
	}
	if err := db.Create(&[]entities.UsageIdentity{
		{Name: "Shared label", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-1", CreatedAt: end, UpdatedAt: end},
		{Name: "Shared label", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-1", CreatedAt: end, UpdatedAt: end},
	}).Error; err != nil {
		t.Fatalf("seed comparison identities: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return queryNow }})
	if err != nil {
		t.Fatalf("load recent events: %v", err)
	}
	t.Cleanup(cache.Close)
	filter := repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end, QueryNow: &queryNow, EndExclusive: true}
	for _, testCase := range []struct {
		name   string
		source *repository.UsageRecentEventCache
	}{{"cache", cache}, {"database", nil}} {
		t.Run(testCase.name, func(t *testing.T) {
			var queries *[]string
			if testCase.source != nil {
				queries = captureOverviewDataQueries(t, db, "comparison_recent_cache")
			}
			result, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, testCase.source)
			if err != nil {
				t.Fatalf("build comparison: %v", err)
			}
			comparison := result.Comparisons
			if len(comparison.Models) != 2 || !finiteStoredOverviewTestCost(comparison.Models["model-a"].CostUSD, 6.25) || !comparison.Models["model-a"].CostAvailable || comparison.Models["model-a"].Requests != 2 {
				t.Fatalf("real model and alias grouping changed: %+v", comparison.Models)
			}
			if !finiteStoredOverviewTestCost(comparison.APIKeys["deleted-key"].CostUSD, 6.25) || comparison.APIKeys["deleted-key"].Requests != 2 || comparison.APIKeys["deleted-key"].Key != "deleted-key" {
				t.Fatalf("deleted Key historical identity lost: %+v", comparison.APIKeys)
			}
			if !finiteStoredOverviewTestCost(comparison.AuthFiles["auth-1"].CostUSD, 2.5) || comparison.AuthFiles["auth-1"].Label != "Shared label" || len(comparison.AuthFiles) != 1 {
				t.Fatalf("auth-file cost or identity changed: %+v", comparison.AuthFiles)
			}
			if !finiteStoredOverviewTestCost(comparison.AIProviders["provider-1"].CostUSD, 3.75) || comparison.AIProviders["provider-1"].Label != "Shared label" || len(comparison.AIProviders) != 1 {
				t.Fatalf("provider cost or identity changed: %+v", comparison.AIProviders)
			}
			if !finiteStoredOverviewTestCost(comparison.Models["model-b"].CostUSD, 0) || comparison.Models["model-b"].CostAvailable || comparison.APIKeys["other-key"].CostAvailable {
				t.Fatalf("explicit unavailable fee became available: models=%+v keys=%+v", comparison.Models, comparison.APIKeys)
			}
			if queries != nil && strings.Contains(strings.Join(*queries, "\n"), "usage_events") {
				t.Fatalf("covered comparison boundary fell back to hot table: %v", *queries)
			}
			filter.APIGroupKey = "deleted-key"
			filtered, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, testCase.source)
			if err != nil || len(filtered.Comparisons.APIKeys) != 1 || len(filtered.Comparisons.Models) != 1 {
				t.Fatalf("comparison Key filter leaked: result=%+v err=%v", filtered, err)
			}
			filter.APIGroupKey = ""
		})
	}
}

// 完整日桶不扫描热表；不同旧别名归到真实 model，部分 NULL 会阻止费用输出。
func TestOverviewComparisonDailyStoredFeeRequiresCompleteRows(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	zeroUnavailable := int64(0)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: start, APIGroupKey: "old-key", Model: "model-a", ModelAlias: "old-a", AuthIndex: "auth-a", RequestCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: overviewCostPtr(2.75), UnavailableCostCount: &zeroUnavailable},
		{BucketStart: start, APIGroupKey: "old-key", Model: "model-a", ModelAlias: "old-b", AuthIndex: "auth-a", RequestCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: overviewCostPtr(3.5), UnavailableCostCount: &zeroUnavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed daily comparison rows: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop hot events: %v", err)
	}
	filter := repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true}
	result, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatalf("read daily comparison without hot events: %v", err)
	}
	if len(result.Comparisons.Models) != 1 || !finiteStoredOverviewTestCost(result.Comparisons.Models["model-a"].CostUSD, 6.25) || result.Comparisons.Models["model-a"].Requests != 2 {
		t.Fatalf("daily stored fee or real model grouping changed: %+v", result.Comparisons.Models)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: start, APIGroupKey: "old-key", Model: "model-a", ModelAlias: "pending", AuthIndex: "auth-a", RequestCount: 1}).Error; err != nil {
		t.Fatalf("seed partially unbackfilled daily row: %v", err)
	}
	if _, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil); err == nil || !strings.Contains(err.Error(), "unbackfilled cost") {
		t.Fatalf("partially NULL comparison group was accepted: %v", err)
	}
}

// 比较边界的缓存和数据库来源都不能把旧事件的 NULL 金额当作已知零价。
func TestOverviewComparisonBoundaryRejectsUnbackfilledStoredFee(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	end := time.Date(2026, 9, 12, 12, 30, 0, 0, time.UTC)
	start := end.Add(-30 * time.Minute)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "old-null", Timestamp: start.Add(10 * time.Minute), APIGroupKey: "old-key", Model: "model-a", InputTokens: 10, TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("seed unbackfilled comparison event: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end.Add(time.Minute) }})
	if err != nil {
		t.Fatalf("load recent events: %v", err)
	}
	t.Cleanup(cache.Close)
	filter := repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end}
	for _, source := range []*repository.UsageRecentEventCache{cache, nil} {
		if _, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, source); err == nil || !strings.Contains(err.Error(), "unbackfilled cost") {
			t.Fatalf("comparison accepted NULL boundary fee from cache=%t: %v", source != nil, err)
		}
	}
}
