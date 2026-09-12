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

func BenchmarkOverviewComparisons(b *testing.B) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), "overview.db")})
	if err != nil {
		b.Fatal(err)
	}
	sqlDB, _ := db.DB()
	b.Cleanup(func() { _ = sqlDB.Close() })
	end := time.Date(2026, 9, 12, 0, 0, 0, 0, time.Local)
	rows := make([]entities.UsageOverviewDailyStat, 0, 365*12*4*2)
	for day := 1; day <= 365; day++ {
		for key := 0; key < 12; key++ {
			for model := 0; model < 4; model++ {
				for identity := 0; identity < 2; identity++ {
					rows = append(rows, entities.UsageOverviewDailyStat{BucketStart: end.AddDate(0, 0, -day), APIGroupKey: fmt.Sprintf("key-%d", key), Model: fmt.Sprintf("model-%d", model), AuthIndex: fmt.Sprintf("auth-%d", identity), RequestCount: 10000, SuccessCount: 9900, FailureCount: 100, InputTokens: 800000, OutputTokens: 200000, CacheReadTokens: 400000, TotalTokens: 1000000})
				}
			}
		}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		b.Fatal(err)
	}
	for _, days := range []int{30, 365} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("days_%d/comparisons_%t", days, enabled), func(b *testing.B) {
				start := end.AddDate(0, 0, -days)
				filter := repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, IncludeComparisons: enabled, ComparisonOnly: enabled}
				resolver := emptyPricingResolverForTest()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					result, err := repository.BuildUsageOverviewWithFilter(db, filter, resolver)
					if err != nil {
						b.Fatal(err)
					}
				if !enabled && result.Usage.TotalRequests != int64(days*12*4*2*10000) {
						b.Fatal("lost requests")
					}
				}
			})
		}
	}
}
