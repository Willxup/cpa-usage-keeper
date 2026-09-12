package test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func BenchmarkRealtimeCachedInsights(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("events_%d", count), func(b *testing.B) {
			db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), "realtime.db")})
			if err != nil {
				b.Fatal(err)
			}
			sqlDB, _ := db.DB()
			b.Cleanup(func() { _ = sqlDB.Close() })
			end := time.Now().Truncate(time.Second)
			events := make([]entities.UsageEvent, count)
			for i := range events {
				ttft := int64(200 + i%500)
				events[i] = entities.UsageEvent{EventKey: fmt.Sprintf("event-%d", i), Timestamp: end.Add(-time.Duration(i%899+1) * time.Second), APIGroupKey: fmt.Sprintf("key-%d", i%12), Model: fmt.Sprintf("model-%d", i%4), Failed: i%20 == 0, InputTokens: 800, OutputTokens: 200, TotalTokens: 1000, CacheReadTokens: 400, TTFTMS: &ttft, LatencyMS: 2000 + int64(i%1000)}
			}
			if err := db.CreateInBatches(events, 100).Error; err != nil {
				b.Fatal(err)
			}
			cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(cache.Close)
			resolver := emptyPricingResolverForTest()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end}, cache, resolver)
				if err != nil {
					b.Fatal(err)
				}
				if result.Insights.Summary.Requests != int64(count) || len(result.Insights.Outcomes) != 30 {
					b.Fatal("incorrect insights")
				}
			}
		})
	}
}
