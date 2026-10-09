package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestAggregateUsageIdentityStatsTracksCanonicalCacheReadSeparately(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	identity := entities.UsageIdentity{
		Name:         "OpenAI",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "identity-cache-read",
		Type:         "openai",
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	if err := db.Create([]entities.UsageEvent{
		{EventKey: "fallback", AuthType: "apikey", AuthIndex: identity.Identity, Timestamp: now, CachedTokens: 100, CacheReadTokens: 100},
		{EventKey: "explicit", AuthType: "apikey", AuthIndex: identity.Identity, Timestamp: now.Add(time.Second), CachedTokens: 30, CacheReadTokens: 80},
	}).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	// 同一批事件重复聚合仍必须保持缓存统计和游标不变。
	for range 2 {
		if err := repository.AggregateUsageIdentityStats(context.Background(), db, now); err != nil {
			t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
		}
		if err := db.First(&identity, identity.ID).Error; err != nil {
			t.Fatalf("reload usage identity: %v", err)
		}
		if identity.CachedTokens != 130 || identity.CacheReadTokens != 180 || identity.LastAggregatedUsageEventID != 2 {
			t.Fatalf("unexpected identity cache aggregation: %+v", identity)
		}
	}

}
