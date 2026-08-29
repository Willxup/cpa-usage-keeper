package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageRealtimeDBPathMapsRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Now().Truncate(time.Second)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "realtime-priority", Timestamp: end.Add(-time.Minute), APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed realtime event: %v", err)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end}, resolver)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 || realtime.CurrentUsage.Models[0].CostUSD == nil || *realtime.CurrentUsage.Models[0].CostUSD != 2 {
		t.Fatalf("unexpected realtime model cost: %+v", realtime.CurrentUsage.Models)
	}
}

func TestUsageRealtimeRecentCachePathMapsRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Now().Truncate(time.Second)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "realtime-cache-priority", Timestamp: end.Add(-time.Minute), APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed realtime cache event: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache: %v", err)
	}
	t.Cleanup(cache.Close)
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
	realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end}, cache, resolver)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache: %v", err)
	}
	if len(realtime.CurrentUsage.Models) != 1 || realtime.CurrentUsage.Models[0].CostUSD == nil || *realtime.CurrentUsage.Models[0].CostUSD != 2 {
		t.Fatalf("unexpected realtime cache model cost: %+v", realtime.CurrentUsage.Models)
	}
}
