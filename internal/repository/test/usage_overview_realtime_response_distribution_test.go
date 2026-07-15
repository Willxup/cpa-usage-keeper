package test

import (
	"runtime"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildUsageOverviewRealtimeWithFilterRequiresValidTTFTAndLatencyForBothDistributions(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	validTTFT := int64(100)
	zeroTTFT := int64(0)
	ttftWithoutLatency := int64(200)

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "valid-response", Timestamp: now.Add(-4 * time.Minute), LatencyMS: 500, TTFTMS: &validTTFT},
		{EventKey: "zero-ttft", Timestamp: now.Add(-3 * time.Minute), LatencyMS: 600, TTFTMS: &zeroTTFT},
		{EventKey: "zero-latency", Timestamp: now.Add(-2 * time.Minute), LatencyMS: 0, TTFTMS: &ttftWithoutLatency},
		{EventKey: "missing-ttft", Timestamp: now.Add(-time.Minute), LatencyMS: 700},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter returned error: %v", err)
	}

	assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.TTFT, 100)
	assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.Latency, 500)
}

func TestBuildUsageOverviewRealtimeExcludesPrewarmFromResponseDistributions(t *testing.T) {
	testCases := []struct {
		name     string
		useCache bool
	}{
		{name: "database"},
		{name: "recent cache", useCache: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
			normalTTFT := int64(100)
			prewarmTTFT := int64(20)
			generate := true
			prewarm := false
			if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
				{EventKey: "normal-response", Generate: &generate, Timestamp: now.Add(-2 * time.Minute), LatencyMS: 500, TTFTMS: &normalTTFT},
				{EventKey: "prewarm-response", Generate: &prewarm, Timestamp: now.Add(-time.Minute), LatencyMS: 70, TTFTMS: &prewarmTTFT},
			}); err != nil {
				t.Fatalf("InsertUsageEvents returned error: %v", err)
			}

			filter := repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &now}
			var (
				realtime repodto.UsageOverviewRealtimeRecord
				err      error
			)
			if testCase.useCache {
				cache, cacheErr := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
				if cacheErr != nil {
					t.Fatalf("NewUsageRecentEventCache returned error: %v", cacheErr)
				}
				t.Cleanup(cache.Close)
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, cache)
			} else {
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilter(db, filter)
			}
			if err != nil {
				t.Fatalf("build realtime overview: %v", err)
			}

			assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.TTFT, 100)
			assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.Latency, 500)
			var maxRequestCount int64
			for _, point := range realtime.RequestLevel {
				if point.Requests > maxRequestCount {
					maxRequestCount = point.Requests
				}
			}
			if maxRequestCount != 2 {
				t.Fatalf("expected prewarm to remain in rolling request counts, got max=%d", maxRequestCount)
			}
		})
	}
}

func TestUsageRecentEventCacheTryAppendCopiesGeneratePointer(t *testing.T) {
	previousMaxProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousMaxProcs) })

	db := openTestDatabase(t)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	generate := false
	if !cache.TryAppend([]entities.UsageEvent{{
		EventKey:  "async-prewarm",
		Timestamp: now,
		Generate:  &generate,
	}}) {
		t.Fatal("expected async append to be accepted")
	}
	// TryAppend 返回后调用方可以复用原始事件；缓存必须持有独立的 Generate 值。
	generate = true

	deadline := time.Now().Add(time.Second)
	for {
		events, ok := cache.Events(now.Add(-time.Minute), now.Add(time.Minute), false, "")
		if !ok {
			t.Fatal("expected recent cache to remain available")
		}
		if len(events) == 1 {
			if events[0].Generate {
				t.Fatal("expected async cache append to preserve the original generate=false value")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for async cache append, got %d events", len(events))
		}
		runtime.Gosched()
	}
}

func assertRealtimeResponseDistributionUsesOnlyValidPairs(t *testing.T, series repodto.RealtimeResponseDistributionSeriesRecord, wantMS int64) {
	t.Helper()
	if series.TotalParticles != 1 || len(series.Particles) != 1 {
		t.Fatalf("expected one valid response sample, got total=%d particles=%+v", series.TotalParticles, series.Particles)
	}
	if series.Particles[0].MS != wantMS {
		t.Fatalf("expected valid response sample %dms, got %+v", wantMS, series.Particles[0])
	}

	nonEmptyAveragePoints := 0
	for _, point := range series.AverageLine {
		if point.AvgMS == nil {
			continue
		}
		nonEmptyAveragePoints++
		if *point.AvgMS != float64(wantMS) {
			t.Fatalf("expected average line to use only %dms samples, got %+v", wantMS, point)
		}
	}
	if nonEmptyAveragePoints == 0 {
		t.Fatal("expected average line to contain the valid response sample")
	}
}
