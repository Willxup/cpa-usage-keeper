package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestOverviewComparisonIdentitySeriesPreservesBoundariesAndDistinctIdentities(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 9, 12, 8, 30, 0, 0, time.Local)
	end := start.Add(4 * time.Hour)
	identities := []entities.UsageIdentity{
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "file-a", Name: "same.json"},
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "file-b", Name: "same.json"},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-a", Name: "OpenAI"},
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	events := []entities.UsageEvent{
		{EventKey: "file-left", Timestamp: start, APIGroupKey: "key-a", Model: "model-a", AuthIndex: "file-a", TotalTokens: 10},
		{EventKey: "file-rollup", Timestamp: start.Add(time.Hour), APIGroupKey: "key-a", Model: "model-a", AuthIndex: "file-b", TotalTokens: 20},
		{EventKey: "provider-rollup", Timestamp: start.Add(2 * time.Hour), APIGroupKey: "key-b", Model: "model-b", AuthIndex: "provider-a", TotalTokens: 30},
		{EventKey: "provider-right", Timestamp: end.Add(-time.Minute), APIGroupKey: "key-b", Model: "model-b", AuthIndex: "provider-a", TotalTokens: 40},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	filter := dto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true}
	overview, err := repository.BuildUsageOverviewWithFilter(db, filter, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	c := overview.Comparisons
	if len(c.AuthFiles) != 2 || len(c.AIProviders) != 1 {
		t.Fatalf("identity collision: %+v", c)
	}
	for _, dimension := range []map[string]*dto.UsageComparisonItemRecord{c.Models, c.APIKeys, c.AuthFiles, c.AIProviders} {
		for _, item := range dimension {
			var sum int64
			for _, bucket := range c.Buckets {
				sum += item.TokenBuckets[bucket]
			}
			if sum != item.TotalTokens {
				t.Fatalf("timeline total %d != %d for %s", sum, item.TotalTokens, item.Key)
			}
		}
	}
	if c.AuthFiles["file-a"].TokenBuckets[c.Buckets[0]] != 10 || c.AuthFiles["file-b"].TokenBuckets[c.Buckets[1]] != 20 || c.AIProviders["provider-a"].TokenBuckets[c.Buckets[4]] != 40 {
		t.Fatal("identity buckets lost boundary/rollup data")
	}
	filter.APIGroupKey = "key-a"
	scoped, err := repository.BuildUsageOverviewWithFilter(db, filter, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Comparisons.AIProviders) != 0 || len(scoped.Comparisons.APIKeys) != 1 {
		t.Fatal("scoped timeline leaked another key")
	}
}

func TestOverviewComparisonSeriesIncludesOpenEndedCacheBuckets(t *testing.T) {
	for _, tc := range []struct {
		name, span  string
		hours, hour int
	}{
		{"hour", "5h", 5, 12}, {"midnight-hour", "5h", 5, 23}, {"midnight-day", "7d", 168, 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			end := time.Date(2026, 9, 12, tc.hour, 59, 59, 0, time.Local)
			start := end.Add(-time.Duration(tc.hours) * time.Hour)
			latest := end.Add(2 * time.Second)
			ids := []entities.UsageIdentity{{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "file-a", Name: "file.json"}, {AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-a", Name: "Provider"}}
			if err := db.Create(&ids).Error; err != nil {
				t.Fatal(err)
			}
			events := []entities.UsageEvent{
				{EventKey: "old", Timestamp: end.Add(-time.Second), APIGroupKey: "key-a", Model: "model", AuthIndex: "file-a", TotalTokens: 10},
				{EventKey: "new-file", Timestamp: latest, APIGroupKey: "key-a", Model: "model", AuthIndex: "file-a", TotalTokens: 20},
				{EventKey: "new-provider", Timestamp: latest, APIGroupKey: "key-a", Model: "model", AuthIndex: "provider-a", TotalTokens: 30},
				{EventKey: "other-key", Timestamp: latest.Add(time.Hour), APIGroupKey: "key-b", Model: "other-model", TotalTokens: 999},
			}
			if err := db.Create(&events).Error; err != nil {
				t.Fatal(err)
			}
			cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return latest }})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			filter := dto.UsageQueryFilter{Range: tc.span, StartTime: &start, EndTime: &end, QueryNow: &end, APIGroupKey: "key-a", ComparisonOnly: true}
			result, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, cache, emptyPricingResolverForTest())
			if err != nil {
				t.Fatal(err)
			}
			c := result.Comparisons
			if c.Models["model"].TotalTokens != 60 {
				t.Fatalf("latest cache events must still count: %+v", c.Models)
			}
			expected := latest.Truncate(time.Hour).Format(time.RFC3339Nano)
			if tc.hours > 24 {
				expected = latest.Format(time.DateOnly)
			}
			if c.Buckets[len(c.Buckets)-1] != expected {
				t.Fatalf("missing latest cache bucket: got %v, want last %s", c.Buckets, expected)
			}
			for _, dimension := range []map[string]*dto.UsageComparisonItemRecord{c.Models, c.APIKeys, c.AuthFiles, c.AIProviders} {
				for _, item := range dimension {
					var sum int64
					for _, bucket := range c.Buckets {
						sum += item.TokenBuckets[bucket]
					}
					if sum != item.TotalTokens {
						t.Fatalf("series %d != total %d for %s", sum, item.TotalTokens, item.Key)
					}
				}
			}
			filter.ComparisonOnly = false
			main, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, cache, emptyPricingResolverForTest())
			if err != nil {
				t.Fatal(err)
			}
			if main.Usage.TotalTokens != c.Models["model"].TotalTokens {
				t.Fatal("comparisons diverged from main overview")
			}
			filter.ComparisonOnly = true
			historicalNow := latest.Add(time.Hour)
			filter.QueryNow = &historicalNow
			filter.Range = "custom"
			historical, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, cache, emptyPricingResolverForTest())
			if err != nil {
				t.Fatal(err)
			}
			if historical.Comparisons.Models["model"].TotalTokens != 10 {
				t.Fatal("historical end boundary must not include newer cache events")
			}
		})
	}
}
