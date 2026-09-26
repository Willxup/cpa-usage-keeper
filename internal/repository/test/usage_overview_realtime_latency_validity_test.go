package test

import (
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildUsageOverviewRealtimeScatterRequiresValidTTFTAndLatency(t *testing.T) {
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
	}, emptyPricingResolverForTest())

	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter returned error: %v", err)
	}

	assertRealtimeLatencyScatterPair(t, realtime.LatencyScatter, 100, 500)
}

func TestBuildUsageOverviewRealtimeScatterExcludesPrewarmAndNongenerate(t *testing.T) {
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
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, cache, emptyPricingResolverForTest())
			} else {
				realtime, err = repository.BuildUsageOverviewRealtimeWithFilter(db, filter, emptyPricingResolverForTest())
			}
			if err != nil {
				t.Fatalf("build realtime overview: %v", err)
			}

			assertRealtimeLatencyScatterPair(t, realtime.LatencyScatter, 100, 500)
			var maxRequestCount int64
			for _, point := range realtime.RequestLevel {
				maxRequestCount = max(maxRequestCount, point.Requests)
			}
			if maxRequestCount != 2 {
				t.Fatalf("expected prewarm to remain in rolling request counts, got max=%d", maxRequestCount)
			}
		})
	}
}

func assertRealtimeLatencyScatterPair(t *testing.T, scatter repodto.RealtimeLatencyScatterRecord, ttftMS, latencyMS int64) {
	t.Helper()
	if scatter.TotalPoints != 1 || len(scatter.Points) != 1 ||
		scatter.Points[0].TTFTMS != ttftMS || scatter.Points[0].LatencyMS != latencyMS ||
		scatter.P95TTFTMS != ttftMS || scatter.P95LatencyMS != latencyMS ||
		scatter.MaxTTFTMS != ttftMS || scatter.MaxLatencyMS != latencyMS {
		t.Fatalf("expected one valid request pair with full summary, got %+v", scatter)
	}
}

func TestUsageRecentEventCacheTryAppendCopiesGeneratePointer(t *testing.T) {
	previousMaxProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousMaxProcs) })

	db := openTestDatabase(t)
	synctest.Test(t, func(t *testing.T) {
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

		synctest.Wait()
		events, ok := cache.Events(now.Add(-time.Minute), now.Add(time.Minute), false, "")
		if !ok || len(events) != 1 || events[0].Generate {
			t.Fatalf("expected async append to retain generate=false, got available=%v events=%+v", ok, events)
		}
	})
}
