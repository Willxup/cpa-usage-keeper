package test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestCredentialHealthStartupMatchesAppendAcrossTimezones(t *testing.T) {
	for _, tc := range []struct{ zone, now string }{
		{"Europe/London", "2026-10-25T05:30:00Z"},
		{"Europe/Berlin", "2026-10-25T05:30:00Z"},
		{"America/New_York", "2026-11-01T10:30:00Z"},
		{"Europe/London", "2026-03-29T05:30:00Z"},
		{"UTC", "2026-10-25T05:30:00Z"},
		{"Asia/Shanghai", "2026-10-25T05:30:00Z"},
	} {
		t.Run(tc.zone+tc.now, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			now = now.In(location)
			start := now.Add(-5 * time.Hour)
			db := openTestDatabase(t)
			options := repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }}
			appended, err := repository.NewUsageRecentEventCache(db, options)
			if err != nil {
				t.Fatal(err)
			}
			defer appended.Close()
			var events []entities.UsageEvent
			for i, at := range []time.Time{start.Add(-15 * time.Minute), start, start.Add(45 * time.Minute), now.Add(-time.Minute), now.Add(11 * time.Minute)} {
				events = append(events, entities.UsageEvent{
					EventKey: fmt.Sprintf("health-time-%d", i), Timestamp: at,
					AuthType: "oauth", AuthIndex: "shared", Failed: i == 3,
					InputTokens: 10, CacheReadTokens: 4,
				})
			}
			other := events[2]
			other.EventKey, other.AuthType, other.InputTokens = "health-other", "apikey", 999
			events = append(events, other)
			if err := db.Create(&events).Error; err != nil {
				t.Fatal(err)
			}
			var batchSizes []int
			if err := loadCredentialHealthCacheRowsBatched(db, start, now, 2, func(rows opaqueCredentialHealthRows) error {
				batchSizes = append(batchSizes, len(rows))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// Keep the exact lower bound and open upper bound, filtering stale candidates
			// before batching, even when the SQL read spans repeated local hours.
			if !reflect.DeepEqual(batchSizes, []int{2, 2, 1}) {
				t.Fatalf("unexpected startup batches: %v", batchSizes)
			}
			if !appended.TryAppend(events) {
				t.Fatal("append rejected")
			}
			if err := appended.DrainAcceptedAppends(context.Background()); err != nil {
				t.Fatal(err)
			}
			cold, err := repository.NewUsageRecentEventCache(db, options)
			if err != nil {
				t.Fatal(err)
			}
			defer cold.Close()
			for index, at := range []time.Time{now, now.Add(12 * time.Minute)} {
				for _, authType := range []string{"oauth", "apikey"} {
					want, ok := appended.CredentialHealth(authType, "shared", at)
					if !ok {
						t.Fatal("append health unavailable")
					}
					if authType == "oauth" && (want.TotalSuccess != int64(1+index) || want.TotalFailure != 1 || want.InputTokens != int64(20+10*index) || want.CacheReadTokens != int64(8+4*index)) {
						t.Fatalf("unexpected reference health: %+v", want)
					}
					if authType == "apikey" && (want.TotalSuccess != 1 || want.InputTokens != 999) {
						t.Fatalf("unexpected isolated identity: %+v", want)
					}
					got, ok := cold.CredentialHealth(authType, "shared", at)
					if !ok || !reflect.DeepEqual(got, want) {
						t.Errorf("%s at %s: cold start differs from append (success %d/%d, failure %d/%d, input %d/%d, cache read %d/%d)", authType, at, got.TotalSuccess, want.TotalSuccess, got.TotalFailure, want.TotalFailure, got.InputTokens, want.InputTokens, got.CacheReadTokens, want.CacheReadTokens)
					}
				}
			}
		})
	}
}

func TestCredentialHealthTokenTotalsSaturateOnInt64Overflow(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)

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
