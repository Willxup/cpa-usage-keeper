package legacy

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// BenchmarkUsageOverviewStatsBacked 衡量 Overview 查询走增量 stats 后的窗口查询成本。
func BenchmarkUsageOverviewStatsBacked(b *testing.B) {
	for _, size := range []int{1_000, 10_000} {
		for _, window := range overviewBenchmarkWindows() {
			b.Run(fmt.Sprintf("events_%d_%s", size, window.name), func(b *testing.B) {
				db := openOverviewBenchmarkDB(b, size)
				filter := repositorydto.UsageQueryFilter{Range: window.name, StartTime: &window.start, EndTime: &window.end}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := repository.BuildUsageOverviewWithFilter(db, filter, pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()); err != nil {
						b.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkUsageOverviewRawEventScan 保留旧的事件扫描基线，便于和 stats-backed 查询对照。
func BenchmarkUsageOverviewRawEventScan(b *testing.B) {
	for _, size := range []int{1_000, 10_000} {
		for _, window := range overviewBenchmarkWindows() {
			b.Run(fmt.Sprintf("events_%d_%s", size, window.name), func(b *testing.B) {
				db := openOverviewBenchmarkDBWithoutStats(b, size)
				b.Cleanup(func() { closeOverviewBenchmarkDB(b, db) })
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					record := rawUsageOverviewBenchmarkScan(b, db, window.start, window.end)
					if record.requests == 0 {
						b.Fatalf("raw scan returned no requests")
					}
				}
			})
		}
	}
}

type overviewBenchmarkWindow struct {
	name  string
	start time.Time
	end   time.Time
}

type rawUsageOverviewBenchmarkRecord struct {
	requests int64
	tokens   int64
}

// overviewBenchmarkWindows 覆盖短窗口、长窗口和非整点 custom 窗口三类查询形态。
func overviewBenchmarkWindows() []overviewBenchmarkWindow {
	end := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	return []overviewBenchmarkWindow{
		{name: "4h", start: end.Add(-4 * time.Hour), end: end},
		{name: "24h", start: end.Add(-24 * time.Hour), end: end},
		{name: "7d", start: end.Add(-7 * 24 * time.Hour), end: end},
		{name: "30d", start: end.Add(-30 * 24 * time.Hour), end: end},
		{name: "custom_partial", start: end.Add(-36*time.Hour - 17*time.Minute), end: end.Add(-11 * time.Minute)},
	}
}

// rawUsageOverviewBenchmarkScan 模拟旧路径只按时间窗口加载 usage_events 后在内存聚合。
func rawUsageOverviewBenchmarkScan(b *testing.B, db *gorm.DB, start, end time.Time) rawUsageOverviewBenchmarkRecord {
	b.Helper()
	var events []entities.UsageEvent
	if err := db.Model(&entities.UsageEvent{}).
		Where("timestamp >= ? AND timestamp < ?", start, end).
		Order("timestamp asc").
		Find(&events).Error; err != nil {
		b.Fatalf("load raw usage events returned error: %v", err)
	}
	record := rawUsageOverviewBenchmarkRecord{requests: int64(len(events))}
	for _, event := range events {
		record.tokens += event.TotalTokens
	}
	return record
}

// BenchmarkUsageOverviewAggregateCatchup 衡量从空 checkpoint 聚合一批 usage_events 的 catch-up 成本。
func BenchmarkUsageOverviewAggregateCatchup(b *testing.B) {
	for _, size := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("events_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				db := openOverviewBenchmarkDBWithoutStats(b, size)
				b.StartTimer()
				if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)); err != nil {
					b.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
				}
				b.StopTimer()
				closeOverviewBenchmarkDB(b, db)
			}
		})
	}
}

func openOverviewBenchmarkDB(b *testing.B, eventCount int) *gorm.DB {
	b.Helper()
	db := openOverviewBenchmarkDBWithoutStats(b, eventCount)
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)); err != nil {
		b.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}
	b.Cleanup(func() { closeOverviewBenchmarkDB(b, db) })
	return db
}

func openOverviewBenchmarkDBWithoutStats(b *testing.B, eventCount int) *gorm.DB {
	b.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), "overview-benchmark.db")})
	if err != nil {
		b.Fatalf("OpenDatabase returned error: %v", err)
	}
	events := make([]entities.UsageEvent, 0, eventCount)
	base := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < eventCount; i++ {
		events = append(events, entities.UsageEvent{
			EventKey:            fmt.Sprintf("bench-%d", i),
			APIGroupKey:         fmt.Sprintf("api-%d", i%8),
			Model:               fmt.Sprintf("model-%d", i%6),
			Timestamp:           base.Add(time.Duration(i) * time.Minute),
			Failed:              i%13 == 0,
			InputTokens:         int64(100 + i%50),
			OutputTokens:        int64(40 + i%25),
			ReasoningTokens:     int64(i % 7),
			CachedTokens:        int64(i % 20),
			CacheReadTokens:     int64(i % 9),
			CacheCreationTokens: int64(i % 5),
			TotalTokens:         int64(140 + i%82),
		})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		b.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	return db
}

func closeOverviewBenchmarkDB(b *testing.B, db *gorm.DB) {
	b.Helper()
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
