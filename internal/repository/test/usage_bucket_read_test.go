package test

import (
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestOverviewReadersKeepRepeatedHoursAndShanghaiResults(t *testing.T) {
	for _, tc := range []struct{ zone, start string }{
		{"Europe/Berlin", "2026-10-25T02:00:00+02:00"},
		{"Asia/Shanghai", "2026-10-25T02:00:00+08:00"},
		{"Asia/Kathmandu", "2026-10-25T02:45:00+05:45"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			old := time.Local
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			time.Local = location
			t.Cleanup(func() { time.Local = old })
			start, err := time.Parse(time.RFC3339, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			start = start.In(location)
			end := start.Add(6 * time.Hour)
			db := openTestDatabase(t)
			if err := db.Create(&entities.CPAAPIKey{APIKey: "bucket-key"}).Error; err != nil {
				t.Fatal(err)
			}
			cost, unavailable := 1.0, int64(0)
			// 独立预置旧版已存行，不调用新版聚合来替代兼容性证据。两侧各放一个排除桶。
			var stored []entities.UsageOverviewHourlyStat
			for i := -1; i <= 6; i++ {
				stored = append(stored, entities.UsageOverviewHourlyStat{
					BucketStart: start.Add(time.Duration(i) * time.Hour), APIGroupKey: "bucket-key", Model: "model",
					RequestCount: 1, SuccessCount: 1, TotalTokens: 10, CostUSD: &cost, UnavailableCostCount: &unavailable,
					SpeedTPSSum: 20, SpeedSampleCount: 1,
				})
			}
			if err := db.Create(&stored).Error; err != nil {
				t.Fatal(err)
			}
			var before []entities.UsageOverviewHourlyStat
			if err := db.Order("id").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			filter := dto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end}
			analysis, err := repository.BuildAnalysisWithFilter(db, filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(analysis.TokenUsage) != 6 || analysis.ModelSpeed.Series[0].SpeedSampleCount != 6 {
				t.Fatalf("missing repeated hour: %+v", analysis.TokenUsage)
			}
			for i, point := range analysis.TokenUsage {
				if !point.Bucket.Equal(start.Add(time.Duration(i)*time.Hour)) || point.TotalTokens != 10 {
					t.Fatalf("point[%d] = %+v", i, point)
				}
			}
			overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			if overview.Usage.TotalRequests != 6 || overview.Usage.TotalTokens != 60 || len(overview.Series.Tokens) != 6 {
				t.Fatalf("unexpected overview: %+v", overview)
			}
			assertFloatClose(t, overview.Summary.TotalCost, 6)
			comparison, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			model := comparison.Comparisons.Models["model"]
			if model == nil || model.TotalTokens != 60 || len(model.TokenBuckets) != 6 {
				t.Fatalf("unexpected comparison: %+v", comparison.Comparisons)
			}
			var after []entities.UsageOverviewHourlyStat
			if err := db.Order("id").Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("read changed stored buckets")
			}
		})
	}
}

func TestOverviewReadersMapLegacyMidnightKeyWithoutRewriting(t *testing.T) {
	previous := time.Local
	location, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	db := openTestDatabase(t)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "midnight-key"}).Error; err != nil {
		t.Fatal(err)
	}
	// 旧版 time.Date 的 9月6日午夜落到9月5日23点，但行内统计属于9月6日。
	legacy := time.Date(2026, 9, 6, 0, 0, 0, 0, location)
	start := time.Date(2026, 9, 6, 1, 0, 0, 0, location)
	end := time.Date(2026, 9, 7, 0, 0, 0, 0, location)
	cost, unavailable := 2.0, int64(0)
	row := entities.UsageOverviewDailyStat{BucketStart: legacy, APIGroupKey: "midnight-key", Model: "model", RequestCount: 2, TotalTokens: 100, SpeedTPSSum: 40, SpeedSampleCount: 2, CostUSD: &cost, UnavailableCostCount: &unavailable}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	filter := dto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end}
	analysis, err := repository.BuildAnalysisWithFilter(db, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(start) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("legacy key was not mapped to its business date: %+v", analysis.TokenUsage)
	}
	if len(analysis.ModelSpeed.Series) != 1 || analysis.ModelSpeed.Series[0].SpeedSampleCount != 2 || len(analysis.ModelSpeed.Buckets) != 1 || !analysis.ModelSpeed.Buckets[0].End.Equal(end) {
		t.Fatalf("lost midnight speed samples: %+v", analysis.ModelSpeed)
	}
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertFloatClose(t, overview.Series.RPM["2026-09-06"], 2.0/(23*60))
	if overview.Series.Tokens["2026-09-06"] != 100 || len(overview.Series.Tokens) != 1 {
		t.Fatalf("unexpected day series: %+v", overview.Series)
	}
	comparison, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Comparisons.Buckets) != 1 || comparison.Comparisons.Buckets[0] != "2026-09-06" {
		t.Fatalf("wrong comparison date enumeration: %+v", comparison.Comparisons.Buckets)
	}
	model := comparison.Comparisons.Models["model"]
	if model == nil || model.TotalTokens != 100 || model.TokenBuckets["2026-09-06"] != 100 || len(model.TokenBuckets) != 1 {
		t.Fatalf("unexpected comparison: %+v", comparison.Comparisons)
	}
	var after entities.UsageOverviewDailyStat
	if err := db.First(&after, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !after.BucketStart.Equal(legacy) || after.TotalTokens != row.TotalTokens || after.SpeedSampleCount != row.SpeedSampleCount {
		t.Fatal("read changed legacy stored row")
	}
}
