package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestRealtimeLatencyScatterKeepsPairsAndFullWindowSummary(t *testing.T) {
	for _, window := range []string{"15m", "30m", "60m"} {
		for _, useCache := range []bool{false, true} {
			name := fmt.Sprintf("%s/cache=%t", window, useCache)
			t.Run(name, func(t *testing.T) {
				db := openTestDatabase(t)
				now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
				span := map[string]time.Duration{"15m": 15 * time.Minute, "30m": 30 * time.Minute, "60m": 60 * time.Minute}[window]
				start := now.Add(-span)
				warmupTTFT := int64(99999)
				events := []entities.UsageEvent{{EventKey: "warmup", APIGroupKey: "viewer-key", Timestamp: start.Add(-time.Second), TTFTMS: &warmupTTFT, LatencyMS: 999999}}
				// 同一时间戳、反向变化的坐标使两个指标独立排序后错误配对立即可见。
				for index := 0; index < 1205; index++ {
					ttft := int64(index + 1)
					events = append(events, entities.UsageEvent{
						EventKey: fmt.Sprintf("pair-%d", index), APIGroupKey: "viewer-key",
						Timestamp: start.Add(time.Minute), TTFTMS: &ttft, LatencyMS: int64(100000 - index),
					})
				}
				invalidTTFT := int64(50000)
				failed := entities.UsageEvent{EventKey: "failed", APIGroupKey: "viewer-key", Timestamp: start.Add(2 * time.Minute), TTFTMS: &invalidTTFT, LatencyMS: 500000, Failed: true}
				other := entities.UsageEvent{EventKey: "other-key", APIGroupKey: "other-key", Timestamp: start.Add(2 * time.Minute), TTFTMS: &invalidTTFT, LatencyMS: 500000}
				events = append(events, failed, other)
				var cache *repository.UsageRecentEventCache
				if useCache {
					cache = newEmptyUsageRecentEventCache(repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
					t.Cleanup(cache.Close)
					appendRecentCacheEvents(cache, events)
					if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
						t.Fatal(err)
					}
				} else if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
					t.Fatal(err)
				}
				realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
					APIGroupKey: "viewer-key", RealtimeWindow: window, RealtimeEndTime: &now,
				}, cache, emptyPricingResolverForTest())
				if err != nil {
					t.Fatal(err)
				}
				scatter := realtime.LatencyScatter
				if scatter.TotalPoints != 1205 || len(scatter.Points) != 1000 || scatter.P95TTFTMS != 1145 || scatter.P95LatencyMS != 99940 || scatter.MaxTTFTMS != 1205 || scatter.MaxLatencyMS != 100000 {
					t.Fatalf("scatter must summarize all 1205 visible requests: %+v", scatter)
				}
				for _, point := range scatter.Points {
					if point.TTFTMS+point.LatencyMS != 100001 {
						t.Fatalf("request coordinates were mismatched: %+v", point)
					}
				}
			})
		}
	}
}

func TestRealtimeLatencyScatterKeepsEveryPointBelowCap(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	ttft := int64(120)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "one", Timestamp: now.Add(-time.Minute), TTFTMS: &ttft, LatencyMS: 800}}); err != nil {
		t.Fatal(err)
	}
	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &now}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatal(err)
	}
	scatter := realtime.LatencyScatter
	if scatter.TotalPoints != 1 || len(scatter.Points) != 1 || scatter.Points[0].TTFTMS != 120 || scatter.Points[0].LatencyMS != 800 {
		t.Fatalf("expected the original request pair below cap, got %+v", scatter)
	}
}
