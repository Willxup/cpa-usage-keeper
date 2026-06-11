package repository

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
)

func TestUsageRecentEventCacheLoadsOnlyRecentProjectionAndDerivesFallbackLabels(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "recent-cache.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	ttft := int64(120)
	events := []entities.UsageEvent{
		{
			EventKey:            "old-event",
			RequestID:           "old-request",
			APIGroupKey:         "provider-a",
			Provider:            "codex",
			AuthType:            "oauth",
			Model:               "gpt-5",
			Timestamp:           now.Add(-61 * time.Minute),
			Source:              "old@example.com",
			AuthIndex:           "auth-old",
			InputTokens:         1,
			OutputTokens:        2,
			ReasoningTokens:     3,
			CachedTokens:        4,
			CacheReadTokens:     5,
			CacheCreationTokens: 6,
			TotalTokens:         7,
		},
		{
			EventKey:    "too-old-event",
			RequestID:   "too-old-request",
			APIGroupKey: "provider-a",
			Provider:    "codex",
			AuthType:    "oauth",
			Model:       "gpt-5",
			Timestamp:   now.Add(-71 * time.Minute),
			Source:      "too-old@example.com",
			AuthIndex:   "auth-too-old",
			TotalTokens: 700,
		},
		{
			EventKey:            "auth-file-event",
			RequestID:           "auth-file-request",
			APIGroupKey:         "provider-a",
			Provider:            "codex",
			AuthType:            "oauth",
			Model:               "gpt-5",
			Timestamp:           now.Add(-30 * time.Minute),
			Source:              "auth-user@example.com",
			AuthIndex:           "auth-1",
			Failed:              true,
			LatencyMS:           500,
			TTFTMS:              &ttft,
			InputTokens:         10,
			OutputTokens:        20,
			ReasoningTokens:     30,
			CachedTokens:        40,
			CacheReadTokens:     50,
			CacheCreationTokens: 60,
			TotalTokens:         70,
		},
		{
			EventKey:    "provider-event",
			RequestID:   "provider-request",
			APIGroupKey: "provider-b",
			Provider:    "Claude Provider",
			AuthType:    "apikey",
			Model:       "claude-sonnet",
			Timestamp:   now.Add(-20 * time.Minute),
			Source:      "sk-provider",
			AuthIndex:   "provider-1",
			TotalTokens: 80,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	cache, err := NewUsageRecentEventCache(db, UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	cached, ok := cache.Events(now.Add(-70*time.Minute), now, false, "")
	if !ok {
		t.Fatal("expected recent cache to cover the default 70 minute window")
	}
	if len(cached) != 3 {
		t.Fatalf("expected 3 recent events in 70 minute cache, got %d: %+v", len(cached), cached)
	}

	oldAuthFile := cached[0]
	if oldAuthFile.AuthIndex != "auth-old" || oldAuthFile.TotalTokens != 7 {
		t.Fatalf("expected default cache window to keep 61 minute old event, got %+v", oldAuthFile)
	}

	authFile := cached[1]
	if authFile.APIGroupKey != "provider-a" || authFile.Model != "gpt-5" || authFile.AuthIndex != "auth-1" {
		t.Fatalf("unexpected auth file event dimensions: %+v", authFile)
	}
	if authFile.IdentityFallbackKind != RecentUsageIdentityAuthFile || authFile.IdentityFallbackLabel != "auth-user@example.com" {
		t.Fatalf("expected auth file fallback to use source, got %+v", authFile)
	}
	if !authFile.Failed || authFile.LatencyMS != 500 || authFile.TTFTMS == nil || *authFile.TTFTMS != 120 {
		t.Fatalf("unexpected auth file latency fields: %+v", authFile)
	}
	if authFile.InputTokens != 10 || authFile.OutputTokens != 20 || authFile.ReasoningTokens != 30 ||
		authFile.CachedTokens != 40 || authFile.CacheReadTokens != 50 || authFile.CacheCreationTokens != 60 || authFile.TotalTokens != 70 {
		t.Fatalf("unexpected auth file token fields: %+v", authFile)
	}

	provider := cached[2]
	if provider.IdentityFallbackKind != RecentUsageIdentityAIProvider || provider.IdentityFallbackLabel != "Claude Provider" {
		t.Fatalf("expected ai provider fallback to use provider, got %+v", provider)
	}
}

func TestUsageRecentEventCacheFiltersByWindowAndAPIGroupKey(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)

	cache.appendEvents([]entities.UsageEvent{
		{APIGroupKey: "provider-a", AuthType: "oauth", Source: "a@example.com", AuthIndex: "auth-a", Model: "gpt-5", Timestamp: now.Add(-10 * time.Minute), TotalTokens: 10},
		{APIGroupKey: "provider-b", AuthType: "apikey", Provider: "Provider B", AuthIndex: "provider-b", Model: "gpt-5", Timestamp: now.Add(-5 * time.Minute), TotalTokens: 20},
	})

	cached, ok := cache.Events(now.Add(-15*time.Minute), now, false, "provider-a")
	if !ok {
		t.Fatal("expected cache to cover recent filtered window")
	}
	if len(cached) != 1 || cached[0].APIGroupKey != "provider-a" || cached[0].TotalTokens != 10 {
		t.Fatalf("unexpected filtered cache events: %+v", cached)
	}

	if oldCached, ok := cache.Events(now.Add(-2*time.Hour), now.Add(-90*time.Minute), false, ""); !ok || len(oldCached) != 0 {
		t.Fatalf("expected cache event filtering to return an empty old window, ok=%v events=%+v", ok, oldCached)
	}
}

func TestUsageRecentEventCacheDefaultQueueSizeAllowsShortBursts(t *testing.T) {
	if usageRecentEventCacheDefaultQueueSize != 100 {
		t.Fatalf("expected recent cache default queue size 100, got %d", usageRecentEventCacheDefaultQueueSize)
	}
}

func TestUsageRecentEventCacheTryAppendDoesNotBlockWhenQueueIsFull(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{
		Now:       func() time.Time { return now },
		QueueSize: 1,
	})
	t.Cleanup(cache.Close)

	<-cache.appendSlots
	if cache.TryAppend([]entities.UsageEvent{{APIGroupKey: "provider-b", AuthType: "oauth", Source: "b@example.com", Timestamp: now}}) {
		t.Fatal("expected append to report queue overflow when no slot is available")
	}
	if len(cache.appendCh) != 0 {
		t.Fatalf("expected overflow append not to enqueue cloned events, got queue length %d", len(cache.appendCh))
	}
	if _, ok := cache.Events(now.Add(-time.Minute), now.Add(time.Minute), false, ""); !ok {
		t.Fatal("expected queue overflow not to invalidate the cache window")
	}
}

func TestUsageRecentEventCacheCloseIsConcurrentSafe(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	for attempt := 0; attempt < 200; attempt++ {
		cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		for index := 0; index < 32; index++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				cache.Close()
			}()
		}
		close(start)
		waitGroup.Wait()

		if cache.TryAppend([]entities.UsageEvent{{APIGroupKey: "provider-a", Timestamp: now}}) {
			t.Fatal("expected append after Close to be rejected")
		}
	}
}
