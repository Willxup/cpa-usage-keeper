package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestComparisonCalendarDayMatchesAnalysisEndpoint(t *testing.T) {
	for _, tc := range []struct {
		zone, date string
		hours      int
	}{
		{"Asia/Shanghai", "2026-03-08", 24},
		{"America/Los_Angeles", "2026-03-08", 23},
		{"America/Los_Angeles", "2026-11-01", 25},
		{"Europe/Berlin", "2026-10-25", 25},
		{"America/Santiago", "2026-09-06", 23},
	} {
		t.Run(tc.zone+"/"+tc.date, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			date, _ := timeutil.ParseCivilDate(tc.date)
			bounds, _ := timeutil.DayBounds(date, location)
			db := openTestDatabase(t)
			cost, unavailable := 1.0, int64(0)
			available := true
			// Include a real next-day row: the display endpoint must not read it.
			for i := 0; i <= tc.hours; i++ {
				row := entities.UsageOverviewHourlyStat{BucketStart: bounds.Start.Add(time.Duration(i) * time.Hour),
					APIGroupKey: "calendar-key", Model: "model", RequestCount: 1, TotalTokens: 100,
					CostUSD: &cost, UnavailableCostCount: &unavailable}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				// The inclusive day's final partial-hour query reads raw events.
				event := entities.UsageEvent{EventKey: fmt.Sprintf("calendar-%d", i), Timestamp: row.BucketStart.Add(time.Minute),
					APIGroupKey: row.APIGroupKey, Model: row.Model, TotalTokens: row.TotalTokens,
					CostUSD: &cost, CostAvailable: &available}
				if err := db.Create(&event).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"today", "yesterday"} {
				anchor := bounds.Start.Add(12 * time.Hour)
				if name == "yesterday" {
					anchor = bounds.End.Add(12 * time.Hour)
				}
				window, err := timeutil.ParseUsageQueryRange(name, "", "", "", anchor)
				if err != nil {
					t.Fatal(err)
				}
				now := bounds.End.Add(48 * time.Hour)
				filter := dto.UsageQueryFilter{Range: name, StartTime: &window.StartTime, EndTime: &window.EndTime, QueryNow: &now}
				result, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
				if err != nil {
					t.Fatal(err)
				}
				comparison := result.Comparisons
				if comparison.Granularity != "hourly" || len(comparison.Buckets) != tc.hours+1 {
					t.Fatalf("%s: expected %d hourly points, got %s/%d", name, tc.hours+1, comparison.Granularity, len(comparison.Buckets))
				}
				analysis, err := repository.BuildAnalysisWithFilter(db, filter)
				if err != nil {
					t.Fatal(err)
				}
				for i, key := range comparison.Buckets {
					if key != timeutil.FormatStorageTime(analysis.TokenUsage[i].Bucket) {
						t.Fatalf("%s: axis differs at %d: %s", name, i, key)
					}
				}
				model := comparison.Models["model"]
				if model == nil || model.TotalTokens != int64(tc.hours*100) || model.Requests != int64(tc.hours) || model.CostUSD != float64(tc.hours) {
					t.Fatalf("%s: changed totals or included next day: %+v", name, model)
				}
				if model.TokenBuckets[timeutil.FormatStorageTime(bounds.End)] != 0 {
					t.Fatal("next-day endpoint must be empty")
				}
				// A query during today still displays the full day, like Analysis.
				if name == "today" {
					filter.QueryNow = &anchor
					current, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
					if err != nil || len(current.Comparisons.Buckets) != tc.hours+1 {
						t.Fatalf("today axis is truncated at query time: %v", err)
					}
				}
			}
		})
	}
}
