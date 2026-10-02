package test

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

func TestUsageServiceGetUsageOverviewDelegatesToFilteredOverview(t *testing.T) {
	withUsageServiceLocation(t, "Asia/Shanghai")

	db := openUsageServiceTestDatabase(t)
	if _, err := repository.UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.3,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), InputTokens: 1000, OutputTokens: 500, CachedTokens: 100, CacheReadTokens: 100, ReasoningTokens: 50, TotalTokens: 1650}, 0.75, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), InputTokens: 500, OutputTokens: 250, CachedTokens: 0, ReasoningTokens: 25, TotalTokens: 775}, 0.25, true),
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC)
	pricingSnapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot returned error: %v", err)
	}
	provider := service.NewUsageServiceWithOptions(db, service.UsageServiceOptions{PricingCatalog: pricing.NewCatalog(pricingSnapshot)})
	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if overview.Usage == nil || overview.Usage.TotalRequests != 2 || overview.Usage.TotalTokens != 2425 {
		t.Fatalf("expected overview usage counts, got %+v", overview.Usage)
	}
	if math.Abs(overview.Summary.RPM-2.0/1440.0) > 0.000000001 || math.Abs(overview.Summary.TPM-2425.0/1440.0) > 0.000000001 {
		t.Fatalf("expected 24h overview rates to use exact 1440 minute window, got %+v", overview.Summary)
	}
	if len(overview.Series.Buckets) != 2 || overview.Series.Buckets[0] != "2026-04-16T17:00:00+08:00" || overview.Series.Buckets[1] != "2026-04-16T18:00:00+08:00" ||
		overview.Series.Requests[0] != 1 || overview.Series.Requests[1] != 1 {
		t.Fatalf("expected hourly request series values, got %+v", overview.Series)
	}
	if !usageFilterCostClose(overview.Series.Cost[0], 0.75) || !usageFilterCostClose(overview.Series.Cost[1], 0.25) || !usageFilterCostClose(overview.Summary.TotalCost, 1) || !overview.Summary.CostAvailable {
		t.Fatalf("expected hourly cost series values, got %+v", overview.Series)
	}
}

func TestUsageServiceOverviewDailyAverageChangesOnlyAfterStoredFeeWriteback(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	if _, err := repository.UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	eventTime := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{storedUsageEventFee(entities.UsageEvent{
		EventKey: "daily-fee", Model: "model-a", Timestamp: eventTime, InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}, 4, true)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, eventTime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	provider := service.NewUsageService(db, catalog)
	start, end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	filter := servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeCount: 2, StartTime: &start, EndTime: &end, EndExclusive: true}
	assertStoredDailyFee := func(wantCost, wantDaily float64) {
		t.Helper()
		result, err := provider.GetUsageOverview(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		if !usageFilterCostClose(result.Summary.TotalCost, wantCost) || !result.Summary.CostAvailable || result.Summary.DailyAverageCost == nil || !usageFilterCostClose(*result.Summary.DailyAverageCost, wantDaily) ||
			len(result.Series.Cost) != 1 || !usageFilterCostClose(result.Series.Cost[0], wantCost) {
			t.Fatalf("daily stored fee mismatch: summary=%+v series=%+v", result.Summary, result.Series)
		}
	}
	assertStoredDailyFee(4, 2)
	// 当前报价从 9 改为 90 后重发快照，旧事件和聚合费用仍必须固定。
	if _, err := repository.UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 90}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(snapshot)
	assertStoredDailyFee(4, 2)
	// 显式写回事件及完整小时／日桶后，新查询才展示新的金额。
	for _, table := range []string{"usage_events", "usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		result := db.Table(table).Where("id > 0").Update("cost_usd", 7)
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatalf("write back %s fee: affected=%d err=%v", table, result.RowsAffected, result.Error)
		}
	}
	assertStoredDailyFee(7, 3.5)
}

func TestUsageServiceGetUsageOverviewUsesRecentCacheForBoundaries(t *testing.T) {
	withUsageServiceLocation(t, "UTC")

	db := openUsageServiceTestDatabase(t)

	now := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{storedUsageEventFee(entities.UsageEvent{
		APIGroupKey:  "provider-a",
		Model:        "gpt-5",
		AuthType:     "oauth",
		Source:       "auth-user@example.com",
		AuthIndex:    "auth-1",
		Timestamp:    start.Add(10 * time.Minute),
		InputTokens:  40,
		OutputTokens: 60,
		TotalTokens:  100,
	}, 0.625, true)})

	provider := service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())
	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{Range: "custom", StartTime: &start, EndTime: &end, QueryNow: &now})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if overview.Usage == nil || overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 100 {
		t.Fatalf("expected overview service to use recent cache boundary event, got %+v", overview.Usage)
	}
	if !usageFilterCostClose(overview.Summary.TotalCost, 0.625) || !overview.Summary.CostAvailable || len(overview.Series.Cost) != 1 || !usageFilterCostClose(overview.Series.Cost[0], 0.625) {
		t.Fatalf("recent boundary fee must come from stored cache event: summary=%+v series=%+v", overview.Summary, overview.Series)
	}
}

func TestUsageServiceGetUsageOverviewRealtimeUsesRecentCache(t *testing.T) {
	db := openUsageServiceTestDatabase(t)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", Source: "auth-user@example.com", AuthIndex: "auth-1", Timestamp: now.Add(-2 * time.Minute), InputTokens: 40, TotalTokens: 100}, 5.75, true),
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "apikey", Provider: "AI Provider", AuthIndex: "provider-1", Timestamp: now.Add(-time.Minute), InputTokens: 20, TotalTokens: 50}, 2.25, true),
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", Source: "auth-user@example.com", AuthIndex: "auth-1", Timestamp: now.Add(-30 * time.Second), Failed: true, InputTokens: 200, TotalTokens: 200}, 99, true),
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", Source: "auth-user@example.com", AuthIndex: "auth-1", Timestamp: now.Add(-15 * time.Second), TotalTokens: 0}, 88, true),
	})
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	provider := service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())
	realtime, err := provider.GetUsageOverviewRealtime(context.Background(), servicedto.UsageFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("GetUsageOverviewRealtime returned error: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 ||
		realtime.CurrentUsage.Models[0].Key != "gpt-5" ||
		realtime.CurrentUsage.Models[0].Tokens != 150 || realtime.CurrentUsage.Models[0].Requests != 4 {
		t.Fatalf("expected realtime service to use recent cache, got %+v", realtime.CurrentUsage.Models)
	}
	if realtime.CurrentUsage.Models[0].CostUSD == nil || !usageFilterCostClose(*realtime.CurrentUsage.Models[0].CostUSD, 8) || realtime.Insights == nil ||
		realtime.Insights.Summary.Requests != 4 || realtime.Insights.Summary.Failures != 1 || realtime.Insights.Summary.TotalTokens != 150 ||
		!usageFilterCostClose(realtime.Insights.Summary.CostUSD, 8) || !realtime.Insights.Summary.CostAvailable {
		t.Fatalf("realtime cache lost stored fee: current=%+v insights=%+v", realtime.CurrentUsage, realtime.Insights)
	}
	if len(realtime.CurrentUsage.APIKeys) != 1 || realtime.CurrentUsage.APIKeys[0].CostUSD == nil || !usageFilterCostClose(*realtime.CurrentUsage.APIKeys[0].CostUSD, 8) ||
		len(realtime.CurrentUsage.AuthFiles) != 1 || realtime.CurrentUsage.AuthFiles[0].CostUSD == nil || !usageFilterCostClose(*realtime.CurrentUsage.AuthFiles[0].CostUSD, 5.75) ||
		len(realtime.CurrentUsage.AIProviders) != 1 || realtime.CurrentUsage.AIProviders[0].CostUSD == nil || !usageFilterCostClose(*realtime.CurrentUsage.AIProviders[0].CostUSD, 2.25) {
		t.Fatalf("realtime four dimensions lost stored fees: %+v", realtime.CurrentUsage)
	}
	seenTrendCost := false
	for _, point := range realtime.TokenVelocity {
		seenTrendCost = seenTrendCost || point.CostUSD != nil && usageFilterCostClose(*point.CostUSD, 8)
	}
	if !seenTrendCost {
		t.Fatalf("realtime trend did not include stored success fee: %+v", realtime.TokenVelocity)
	}
}

func TestUsageServiceRealtimeCacheAndDBFallbackAgreeOnPersistedFees(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{EventKey: "billable", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-2 * time.Minute), InputTokens: 70, TotalTokens: 100}, 2.5, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "failed", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-time.Minute), Failed: true, InputTokens: 500, TotalTokens: 500}, 99, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "zero-token", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-30 * time.Second)}, 88, true),
	}
	cache := newServiceRecentCacheFromEvents(t, db, now, events)
	filter := servicedto.UsageFilter{RealtimeWindow: "15m", RealtimeEndTime: &now}
	for _, test := range []struct {
		name     string
		provider service.UsageProvider
	}{
		{"cache", service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())},
		{"DB fallback", service.NewUsageService(db, emptyPricingCatalogForTest())},
	} {
		t.Run(test.name, func(t *testing.T) {
			realtime, err := test.provider.GetUsageOverviewRealtime(context.Background(), filter)
			if err != nil {
				t.Fatal(err)
			}
			if realtime.Insights == nil || realtime.Insights.Summary.Requests != 3 || realtime.Insights.Summary.Failures != 1 ||
				realtime.Insights.Summary.TotalTokens != 100 || !usageFilterCostClose(realtime.Insights.Summary.CostUSD, 2.5) || !realtime.Insights.Summary.CostAvailable ||
				len(realtime.CurrentUsage.Models) != 1 || realtime.CurrentUsage.Models[0].Requests != 3 || realtime.CurrentUsage.Models[0].Tokens != 100 ||
				realtime.CurrentUsage.Models[0].CostUSD == nil || !usageFilterCostClose(*realtime.CurrentUsage.Models[0].CostUSD, 2.5) {
				t.Fatalf("%s changed realtime stored fee/filter: insights=%+v models=%+v", test.name, realtime.Insights, realtime.CurrentUsage.Models)
			}
		})
	}
}

func TestUsageServiceGetUsageOverviewRealtimeResolvesAPIKeyIDForRecentCache(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	targetID := seedUsageFilterAPIKeys(t, db)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "sk-target-key", Model: "gpt-5", AuthType: "oauth", Source: "target@example.com", AuthIndex: "target-auth", Timestamp: now.Add(-2 * time.Minute), InputTokens: 10, TotalTokens: 30}, 0, false),
		storedUsageEventFee(entities.UsageEvent{APIGroupKey: "sk-other-key", Model: "gpt-5", AuthType: "oauth", Source: "other@example.com", AuthIndex: "other-auth", Timestamp: now.Add(-1 * time.Minute), InputTokens: 100, TotalTokens: 300}, 0, false),
	})

	provider := service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())
	realtime, err := provider.GetUsageOverviewRealtime(context.Background(), servicedto.UsageFilter{
		APIKeyID:        targetID,
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("GetUsageOverviewRealtime returned error: %v", err)
	}
	if len(realtime.CurrentUsage.APIKeys) != 1 ||
		realtime.CurrentUsage.APIKeys[0].Key != "sk-target-key" ||
		realtime.CurrentUsage.APIKeys[0].Tokens != 30 {
		t.Fatalf("expected realtime service to filter cache by resolved API key, got %+v", realtime.CurrentUsage.APIKeys)
	}
}

func TestUsageServiceResolvesAPIKeyIDForUsageQueries(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	targetID := seedUsageFilterAPIKeys(t, db)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{EventKey: "target-1", APIGroupKey: "sk-target-key", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10}, 0.25, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "target-2", APIGroupKey: "sk-target-key", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20}, 0.75, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "other-1", APIGroupKey: "sk-other-key", Model: "claude-other", Timestamp: time.Date(2026, 4, 16, 10, 30, 0, 0, time.UTC), TotalTokens: 300}, 999, true),
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{APIKeyID: targetID, Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if overview.Usage == nil || overview.Usage.TotalRequests != 2 || overview.Usage.TotalTokens != 30 || !usageFilterCostClose(overview.Summary.TotalCost, 1) || !overview.Summary.CostAvailable {
		t.Fatalf("expected overview to use resolved API key and only its stored fees, got usage=%+v summary=%+v", overview.Usage, overview.Summary)
	}
	analysis, err := provider.GetAnalysis(context.Background(), servicedto.UsageFilter{APIKeyID: targetID, Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("GetAnalysis returned error: %v", err)
	}
	if len(analysis.APIKeyComposition) != 1 || analysis.APIKeyComposition[0].Key != "sk-target-key" || analysis.APIKeyComposition[0].TotalTokens != 30 {
		t.Fatalf("expected analysis to use resolved API key, got %+v", analysis.APIKeyComposition)
	}
	events, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{APIKeyID: targetID, Page: 1, PageSize: 100, Limit: 100})
	if err != nil {
		t.Fatalf("ListUsageEvents returned error: %v", err)
	}
	if events.TotalCount != 2 || len(events.Events) != 2 {
		t.Fatalf("expected events to use resolved API key, got %+v", events)
	}
}

func TestUsageServiceRejectsInvalidAPIKeyID(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())

	_, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{APIKeyID: "not-an-id", Page: 1, PageSize: 100, Limit: 100})
	if !errors.Is(err, service.ErrInvalidID) {
		t.Fatalf("expected service.ErrInvalidID, got %v", err)
	}
}

func TestUsageServiceRejectsDeletedAPIKeyID(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	key := entities.CPAAPIKey{APIKey: "sk-deleted-key", IsDeleted: true}
	if err := db.Create(&key).Error; err != nil {
		t.Fatalf("seed deleted API key: %v", err)
	}
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())

	_, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{APIKeyID: strconv.FormatInt(key.ID, 10)})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted key to return record not found, got %v", err)
	}
}

func newServiceRecentCacheFromEvents(t *testing.T, db *gorm.DB, now time.Time, events []entities.UsageEvent) *repository.UsageRecentEventCache {
	t.Helper()
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)
	return cache
}

// usageFilterCostClose 只给测试金额使用普通固定浮点容差；NaN／无穷大不能通过近似断言。
func usageFilterCostClose(got, want float64) bool {
	if math.IsNaN(got) || math.IsInf(got, 0) || math.IsNaN(want) || math.IsInf(want, 0) {
		return false
	}
	return math.Abs(got-want) <= 1e-9+1e-12*math.Max(math.Abs(got), math.Abs(want))
}
