package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageRealtimeMapsRuleDimensions(t *testing.T) {
	for _, useCache := range []bool{false, true} {
		name := "database"
		if useCache {
			name = "recent cache"
		}
		t.Run(name, func(t *testing.T) {
			db := openTestDatabase(t)
			end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
			if err := db.Create(&entities.UsageEvent{
				EventKey: "realtime-priority", Timestamp: end.Add(-time.Minute), APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", InputTokens: 1_000_000, TotalTokens: 1_000_000,
			}).Error; err != nil {
				t.Fatalf("seed realtime event: %v", err)
			}
			resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
			filter := repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end}
			var realtime repodto.UsageOverviewRealtimeRecord
			var err error
			if useCache {
				cache, cacheErr := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
				if cacheErr != nil {
					t.Fatalf("NewUsageRecentEventCache: %v", cacheErr)
				}
				t.Cleanup(cache.Close)
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, cache, resolver)
			} else {
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilter(db, filter, resolver)
			}
			if err != nil {
				t.Fatalf("build realtime overview: %v", err)
			}
			if len(realtime.CurrentUsage.Models) != 1 || realtime.CurrentUsage.Models[0].CostUSD == nil || *realtime.CurrentUsage.Models[0].CostUSD != 2 {
				t.Fatalf("unexpected realtime model cost: %+v", realtime.CurrentUsage.Models)
			}
		})
	}
}
