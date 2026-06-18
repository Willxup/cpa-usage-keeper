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
			ServiceTier:         "priority",
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
			ServiceTier: "standard",
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
	if authFile.APIGroupKey != "provider-a" || authFile.Model != "gpt-5" || authFile.ServiceTier != "priority" || authFile.AuthIndex != "auth-1" {
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
	if provider.ServiceTier != "default" {
		t.Fatalf("expected standard tier to normalize to default, got %+v", provider)
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

func TestUsageRecentEventCacheBuildsCredentialHealthFromStartupAndAppend(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "credential-health-cache.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 34, 0, 0, time.FixedZone("CST", 8*60*60))
	events := []entities.UsageEvent{
		{EventKey: "auth-success", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-23 * time.Minute), Failed: false},
		{EventKey: "auth-failure", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-24 * time.Minute), Failed: true},
		{EventKey: "provider-success", AuthType: "apikey", AuthIndex: "shared-auth", Timestamp: now.Add(-24 * time.Minute), Failed: false},
		{EventKey: "too-old", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-5*time.Hour - time.Minute), Failed: true},
		{EventKey: "blank-auth", AuthType: "oauth", AuthIndex: " ", Timestamp: now.Add(-10 * time.Minute), Failed: true},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	cache, err := NewUsageRecentEventCache(db, UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "auth-latest", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-3 * time.Minute), Failed: false},
	})

	authFileHealth, ok := cache.CredentialHealth("oauth", "shared-auth", now)
	if !ok {
		t.Fatal("expected credential health cache to be available")
	}
	if authFileHealth.WindowSeconds != int64(5*time.Hour/time.Second) || authFileHealth.BucketSeconds != int64(10*time.Minute/time.Second) {
		t.Fatalf("unexpected health window settings: %+v", authFileHealth)
	}
	if len(authFileHealth.Buckets) != 30 {
		t.Fatalf("expected 30 health buckets, got %d", len(authFileHealth.Buckets))
	}
	if !authFileHealth.WindowStart.Equal(time.Date(2026, 6, 10, 7, 40, 0, 0, now.Location())) ||
		!authFileHealth.WindowEnd.Equal(time.Date(2026, 6, 10, 12, 40, 0, 0, now.Location())) {
		t.Fatalf("unexpected health window: start=%s end=%s", authFileHealth.WindowStart, authFileHealth.WindowEnd)
	}

	bucket1210 := findCredentialHealthBucket(t, authFileHealth.Buckets, time.Date(2026, 6, 10, 12, 10, 0, 0, now.Location()))
	if bucket1210.Success != 1 || bucket1210.Failure != 1 {
		t.Fatalf("expected oauth shared-auth 12:10 bucket to have 1 success and 1 failure, got %+v", bucket1210)
	}
	bucket1230 := findCredentialHealthBucket(t, authFileHealth.Buckets, time.Date(2026, 6, 10, 12, 30, 0, 0, now.Location()))
	if bucket1230.Success != 1 || bucket1230.Failure != 0 {
		t.Fatalf("expected oauth shared-auth 12:30 bucket to include appended success, got %+v", bucket1230)
	}
	if authFileHealth.TotalSuccess != 2 || authFileHealth.TotalFailure != 1 {
		t.Fatalf("unexpected oauth shared-auth totals: %+v", authFileHealth)
	}

	providerHealth, ok := cache.CredentialHealth("apikey", "shared-auth", now)
	if !ok {
		t.Fatal("expected provider credential health cache to be available")
	}
	providerBucket := findCredentialHealthBucket(t, providerHealth.Buckets, time.Date(2026, 6, 10, 12, 10, 0, 0, now.Location()))
	if providerBucket.Success != 1 || providerBucket.Failure != 0 {
		t.Fatalf("expected apikey shared-auth to be isolated from oauth shared-auth, got %+v", providerBucket)
	}
	if providerHealth.TotalSuccess != 1 || providerHealth.TotalFailure != 0 {
		t.Fatalf("unexpected apikey shared-auth totals: %+v", providerHealth)
	}

	emptyHealth, ok := cache.CredentialHealth("oauth", "missing-auth", now)
	if !ok {
		t.Fatal("expected missing credential health to still return an empty placeholder")
	}
	if len(emptyHealth.Buckets) != 30 || emptyHealth.TotalSuccess != 0 || emptyHealth.TotalFailure != 0 {
		t.Fatalf("expected empty placeholder health, got %+v", emptyHealth)
	}
	for _, bucket := range emptyHealth.Buckets {
		if bucket.Success != 0 || bucket.Failure != 0 {
			t.Fatalf("expected empty bucket counts, got %+v", bucket)
		}
	}
}

func TestUsageRecentEventCacheCredentialHealthUsesExactIdentityMatch(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "credential-health-exact-match.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 34, 0, 0, time.FixedZone("CST", 8*60*60))
	events := []entities.UsageEvent{
		{EventKey: "exact-auth", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-3 * time.Minute), Failed: false},
		{EventKey: "trimmed-auth", AuthType: "oauth", AuthIndex: " shared-auth ", Timestamp: now.Add(-3 * time.Minute), Failed: false},
		{EventKey: "api-key-alias", AuthType: "api_key", AuthIndex: "shared-auth", Timestamp: now.Add(-3 * time.Minute), Failed: true},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	cache, err := NewUsageRecentEventCache(db, UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	authFileHealth, ok := cache.CredentialHealth("oauth", "shared-auth", now)
	if !ok {
		t.Fatal("expected credential health cache to be available")
	}
	if authFileHealth.TotalSuccess != 1 || authFileHealth.TotalFailure != 0 {
		t.Fatalf("expected only exact oauth shared-auth event to count, got %+v", authFileHealth)
	}

	providerHealth, ok := cache.CredentialHealth("apikey", "shared-auth", now)
	if !ok {
		t.Fatal("expected provider credential health cache to be available")
	}
	if providerHealth.TotalSuccess != 0 || providerHealth.TotalFailure != 0 {
		t.Fatalf("expected alias and trimmed rows not to match apikey shared-auth, got %+v", providerHealth)
	}
}

func TestCredentialHealthStartupLoadStreamsRowsInBatches(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "credential-health-stream.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 25)
	for index := 0; index < 25; index++ {
		events = append(events, entities.UsageEvent{
			EventKey:  "health-stream",
			AuthType:  "oauth",
			AuthIndex: "streamed-auth",
			Timestamp: now.Add(-time.Duration(index) * time.Minute),
			Failed:    index%3 == 0,
		})
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	var batchCount int
	var maxBatchSize int
	var totalRows int
	var failures int
	err = loadCredentialHealthCacheRowsBatched(db, now.Add(-credentialHealthWindow), 10, func(rows []credentialHealthLoadRow) error {
		batchCount++
		if len(rows) > maxBatchSize {
			maxBatchSize = len(rows)
		}
		totalRows += len(rows)
		for _, row := range rows {
			if row.Failed {
				failures++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("loadCredentialHealthCacheRowsBatched returned error: %v", err)
	}

	if batchCount != 3 {
		t.Fatalf("expected 25 rows to load in 3 batches, got %d", batchCount)
	}
	if maxBatchSize > 10 {
		t.Fatalf("expected startup load batch size to stay <= 10, got %d", maxBatchSize)
	}
	if totalRows != 25 {
		t.Fatalf("expected all 25 rows to be streamed, got %d", totalRows)
	}
	if failures != 9 {
		t.Fatalf("expected 9 failed rows, got %d", failures)
	}
}

func TestUsageRecentEventCachePrunesInactiveCredentialHealthKeys(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "credential-health-prune.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	baseNow := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	currentNow := baseNow
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "stale-auth", AuthType: "oauth", AuthIndex: "stale-auth", Timestamp: baseNow.Add(-20 * time.Minute), Failed: false},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	cache, err := NewUsageRecentEventCache(db, UsageRecentEventCacheOptions{Now: func() time.Time { return currentNow }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	staleKey, ok := newCredentialHealthKey("oauth", "stale-auth")
	if !ok {
		t.Fatal("expected stale credential key to be valid")
	}
	if _, ok := cache.credentialHealth.bucketsByCredential[staleKey]; !ok {
		t.Fatalf("expected stale credential to be present before prune, got %+v", cache.credentialHealth.bucketsByCredential)
	}

	currentNow = baseNow.Add(5*time.Hour + 20*time.Minute)
	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "fresh-auth", AuthType: "oauth", AuthIndex: "fresh-auth", Timestamp: currentNow.Add(-time.Minute), Failed: false},
	})

	if _, ok := cache.credentialHealth.bucketsByCredential[staleKey]; ok {
		t.Fatalf("expected inactive stale credential to be pruned after full prune, got %+v", cache.credentialHealth.bucketsByCredential)
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

func TestUsageRecentEventCachePruneClearsRemovedBackingSlots(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{
		Now:    func() time.Time { return now },
		Window: 10 * time.Minute,
	})
	t.Cleanup(cache.Close)

	activeTTFT := int64(120)
	expiredTTFT := int64(900)
	cache.appendEvents([]entities.UsageEvent{
		{APIGroupKey: "active-key-a", AuthType: "oauth", Source: "active-a@example.com", AuthIndex: "active-a", Model: "gpt-5", Timestamp: now.Add(-2 * time.Minute), TTFTMS: &activeTTFT},
		{APIGroupKey: "active-key-b", AuthType: "apikey", Provider: "Active Provider", AuthIndex: "active-b", Model: "claude-sonnet", Timestamp: now.Add(-1 * time.Minute)},
		{APIGroupKey: "expired-key", AuthType: "oauth", Source: "expired@example.com", AuthIndex: "expired-auth", Model: "expired-model", Timestamp: now.Add(-20 * time.Minute), TTFTMS: &expiredTTFT},
	})

	if len(cache.events) != 2 {
		t.Fatalf("expected 2 active events after pruning, got %d: %+v", len(cache.events), cache.events)
	}
	for index, event := range cache.events[:cap(cache.events)] {
		if index < len(cache.events) {
			continue
		}
		if event.APIGroupKey == "expired-key" || event.Model == "expired-model" || event.AuthIndex == "expired-auth" || event.IdentityFallbackLabel == "expired@example.com" || event.TTFTMS != nil {
			t.Fatalf("expected pruned backing slot %d to be cleared, got %+v", index, event)
		}
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

func findCredentialHealthBucket(t *testing.T, buckets []CredentialHealthBucket, start time.Time) CredentialHealthBucket {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.StartTime.Equal(start) {
			return bucket
		}
	}
	t.Fatalf("missing credential health bucket starting at %s in %+v", start, buckets)
	return CredentialHealthBucket{}
}
