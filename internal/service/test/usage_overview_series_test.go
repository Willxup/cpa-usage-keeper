package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestLongCustomOverviewWithoutFactsReturnsEmptySeries(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 31)

	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{
		Range: "custom", CustomUnit: "day", RangeCount: 31,
		StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if len(overview.Series.Buckets) != 0 || len(overview.Series.Requests) != 0 || len(overview.Series.Tokens) != 0 ||
		len(overview.Series.RPM) != 0 || len(overview.Series.TPM) != 0 || len(overview.Series.Cost) != 0 ||
		len(overview.Series.CacheReadRate) != 0 {
		t.Fatalf("expected an empty series for an empty range, got %+v", overview.Series)
	}
}

func TestOverviewSeriesOrdersRepeatedHoursByInstant(t *testing.T) {
	previous := time.Local
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	start, _ := time.Parse(time.RFC3339, "2026-10-25T02:00:00+02:00")
	start = start.In(location)
	end := start.Add(5 * time.Hour)
	db := openUsageServiceTestDatabase(t)
	cost, unavailable := 0.0, int64(0)
	// 稀疏的两个同名小时不补出剩余3点，也不能按字符串把后一个小时放到前面。
	for i := 0; i < 2; i++ {
		row := entities.UsageOverviewHourlyStat{BucketStart: start.Add(time.Duration(i) * time.Hour), APIGroupKey: "key", Model: "model", RequestCount: int64(i + 1), TotalTokens: int64((i + 1) * 10), CostUSD: &cost, UnavailableCostCount: &unavailable}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	result, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{Range: "custom", CustomUnit: "hour", RangeCount: 5, StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Series.Buckets) != 2 {
		t.Fatalf("sparse points changed: %+v", result.Series)
	}
	for i, label := range result.Series.Buckets {
		want := start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		if label != want || result.Series.Tokens[i] != int64((i+1)*10) {
			t.Fatalf("bucket[%d]=%s, want %s", i, label, want)
		}
	}
}

func TestOverviewMergedDayRatesUseWholeCalendarDays(t *testing.T) {
	for _, tc := range []struct {
		zone         string
		firstMinutes float64
	}{
		{"Asia/Shanghai", 2880}, {"America/Los_Angeles", 2820},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			previous := time.Local
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			start := time.Date(2026, 3, 7, 0, 0, 0, 0, location)
			end := start.AddDate(0, 0, 180)
			db := openUsageServiceTestDatabase(t)
			cost, unavailable := 0.0, int64(0)
			var rows []entities.UsageOverviewDailyStat
			for i := 0; i < 180; i++ {
				rows = append(rows, entities.UsageOverviewDailyStat{BucketStart: start.AddDate(0, 0, i), APIGroupKey: "key", Model: "model", RequestCount: 60, TotalTokens: 600, CostUSD: &cost, UnavailableCostCount: &unavailable})
			}
			if err := db.Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
			provider := service.NewUsageService(db, emptyPricingCatalogForTest())
			result, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{Range: "custom", CustomUnit: "day", RangeCount: 180, StartTime: &start, EndTime: &end, EndExclusive: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Series.Buckets) != 90 || result.Series.Requests[0] != 120 || result.Series.Tokens[0] != 1200 {
				t.Fatalf("grouping changed: %+v", result.Series)
			}
			if math.Abs(result.Series.RPM[0]-120/tc.firstMinutes) > 1e-12 || math.Abs(result.Series.TPM[0]-1200/tc.firstMinutes) > 1e-12 {
				t.Fatalf("wrong day denominators: %+v", result.Series)
			}
		})
	}
}
