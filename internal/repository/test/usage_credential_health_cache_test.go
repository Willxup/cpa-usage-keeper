package test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestCredentialHealthTokenTotalsSaturateOnInt64Overflow(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "credential-health-overflow.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{
			EventKey:        "overflow-a",
			AuthType:        "apikey",
			AuthIndex:       "overflow-provider",
			Timestamp:       now.Add(-2 * time.Minute),
			InputTokens:     math.MaxInt64,
			CacheReadTokens: math.MaxInt64,
		},
		{
			EventKey:        "overflow-b",
			AuthType:        "apikey",
			AuthIndex:       "overflow-provider",
			Timestamp:       now.Add(-time.Minute),
			InputTokens:     1,
			CacheReadTokens: 1,
		},
		{
			EventKey:        "overflow-c",
			AuthType:        "apikey",
			AuthIndex:       "overflow-provider",
			Timestamp:       now.Add(-11 * time.Minute),
			InputTokens:     1,
			CacheReadTokens: 1,
		},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	health, ok := cache.CredentialHealth("apikey", "overflow-provider", now)
	if !ok {
		t.Fatal("expected credential health cache to be available")
	}
	if health.InputTokens != math.MaxInt64 || health.CacheReadTokens != math.MaxInt64 {
		t.Fatalf("expected token totals to saturate at MaxInt64, got input=%d cacheRead=%d", health.InputTokens, health.CacheReadTokens)
	}
}
