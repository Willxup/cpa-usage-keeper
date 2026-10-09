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
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), InputTokens: 1000, OutputTokens: 500, CachedTokens: 100, CacheReadTokens: 100, ReasoningTokens: 50, TotalTokens: 1650},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), InputTokens: 500, OutputTokens: 250, CachedTokens: 0, ReasoningTokens: 25, TotalTokens: 775},
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
	if math.Abs(overview.Series.Cost[0]-0.01023) > 0.000000001 || math.Abs(overview.Series.Cost[1]-0.00525) > 0.000000001 {
		t.Fatalf("expected hourly cost series values, got %+v", overview.Series)
	}
}

func TestUsageServiceGetUsageOverviewUsesRecentCacheForBoundaries(t *testing.T) {
	withUsageServiceLocation(t, "UTC")

	db := openUsageServiceTestDatabase(t)

	now := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{{
		APIGroupKey:  "provider-a",
		Model:        "gpt-5",
		AuthType:     "oauth",
		Source:       "auth-user@example.com",
		AuthIndex:    "auth-1",
		Timestamp:    start.Add(10 * time.Minute),
		InputTokens:  40,
		OutputTokens: 60,
		TotalTokens:  100,
	}})

	provider := service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())
	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{Range: "custom", StartTime: &start, EndTime: &end, QueryNow: &now})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if overview.Usage == nil || overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 100 {
		t.Fatalf("expected overview service to use recent cache boundary event, got %+v", overview.Usage)
	}
}

func TestUsageServiceGetUsageOverviewRealtimeUsesRecentCache(t *testing.T) {
	db := openUsageServiceTestDatabase(t)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{{
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "auth-user@example.com",
		AuthIndex:   "auth-1",
		Timestamp:   now.Add(-2 * time.Minute),
		InputTokens: 40,
		TotalTokens: 100,
	}})
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
		realtime.CurrentUsage.Models[0].Tokens != 100 {
		t.Fatalf("expected realtime service to use recent cache, got %+v", realtime.CurrentUsage.Models)
	}
}

func TestUsageServiceGetUsageOverviewRealtimeResolvesAPIKeyIDForRecentCache(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	targetID := seedUsageFilterAPIKeys(t, db)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newServiceRecentCacheFromEvents(t, db, now, []entities.UsageEvent{
		{APIGroupKey: "sk-target-key", Model: "gpt-5", AuthType: "oauth", Source: "target@example.com", AuthIndex: "target-auth", Timestamp: now.Add(-2 * time.Minute), InputTokens: 10, TotalTokens: 30},
		{APIGroupKey: "sk-other-key", Model: "gpt-5", AuthType: "oauth", Source: "other@example.com", AuthIndex: "other-auth", Timestamp: now.Add(-1 * time.Minute), InputTokens: 100, TotalTokens: 300},
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
		{EventKey: "target-1", APIGroupKey: "sk-target-key", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "target-2", APIGroupKey: "sk-target-key", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20},
		{EventKey: "other-1", APIGroupKey: "sk-other-key", Model: "claude-other", Timestamp: time.Date(2026, 4, 16, 10, 30, 0, 0, time.UTC), TotalTokens: 300},
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
	if overview.Usage == nil || overview.Usage.TotalRequests != 2 || overview.Usage.TotalTokens != 30 {
		t.Fatalf("expected overview to use resolved API key, got %+v", overview.Usage)
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
