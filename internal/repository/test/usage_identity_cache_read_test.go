package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestAggregateUsageIdentityStatsTracksCanonicalCacheReadSeparately(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "identity-cache-read.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

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
		{EventKey: "fallback", AuthType: "apikey", AuthIndex: identity.Identity, Timestamp: time.Now(), CachedTokens: 100, CacheReadTokens: 100},
		{EventKey: "explicit", AuthType: "apikey", AuthIndex: identity.Identity, Timestamp: time.Now().Add(time.Second), CachedTokens: 30, CacheReadTokens: 80},
	}).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	if err := repository.AggregateUsageIdentityStats(context.Background(), db, time.Now()); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}
	if err := db.First(&identity, identity.ID).Error; err != nil {
		t.Fatalf("reload usage identity: %v", err)
	}
	if identity.CachedTokens != 130 || identity.CacheReadTokens != 180 || identity.LastAggregatedUsageEventID != 2 {
		t.Fatalf("unexpected identity cache aggregation: %+v", identity)
	}

	if err := repository.AggregateUsageIdentityStats(context.Background(), db, time.Now()); err != nil {
		t.Fatalf("repeat AggregateUsageIdentityStats returned error: %v", err)
	}
	if err := db.First(&identity, identity.ID).Error; err != nil {
		t.Fatalf("reload repeated usage identity: %v", err)
	}
	if identity.CachedTokens != 130 || identity.CacheReadTokens != 180 || identity.LastAggregatedUsageEventID != 2 {
		t.Fatalf("expected idempotent identity cache aggregation, got %+v", identity)
	}
}
