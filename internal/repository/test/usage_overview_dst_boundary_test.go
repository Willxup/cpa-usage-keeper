package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestOverviewRawBoundariesSeparateRepeatedClockTimes(t *testing.T) {
	for _, tc := range []struct {
		name, zone, start, end string
		events                 []string
		want                   int64
	}{
		{"London fractional transition", "Europe/London", "2026-10-25T01:30:00+01:00", "2026-10-25T01:30:00Z", []string{"2026-10-25T01:00:00Z", "2026-10-25T01:00:00.1Z", "2026-10-25T01:00:00.999999999Z"}, 3},
		{"first repeated hour", "Europe/Berlin", "2026-10-25T01:30:00+02:00", "2026-10-25T02:30:00+02:00", []string{"2026-10-25T02:10:00+02:00", "2026-10-25T02:10:00+01:00"}, 1},
		{"crossing backward clock", "Europe/Berlin", "2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00", []string{"2026-10-25T02:10:00+02:00", "2026-10-25T02:40:00+02:00", "2026-10-25T02:10:00+01:00", "2026-10-25T02:40:00+01:00"}, 2},
		{"Shanghai subsecond endpoints", "Asia/Shanghai", "2026-10-25T02:30:00.5+08:00", "2026-10-25T03:30:00.5+08:00", []string{"2026-10-25T02:30:00.4+08:00", "2026-10-25T02:30:00.5+08:00", "2026-10-25T03:30:00.4+08:00", "2026-10-25T03:30:00.5+08:00"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			parse := func(text string) time.Time {
				at, err := time.Parse(time.RFC3339Nano, text)
				if err != nil {
					t.Fatal(err)
				}
				return at.In(location)
			}
			start, end := parse(tc.start), parse(tc.end)
			now := end.Add(48 * time.Hour)
			db := openTestDatabase(t)
			cost, available := 0.5, true
			for i, at := range tc.events {
				row := entities.UsageEvent{EventKey: fmt.Sprintf("boundary-%d", i), APIGroupKey: "key", Model: "model", Timestamp: parse(at), TotalTokens: 10, CostUSD: &cost, CostAvailable: &available}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			filter := dto.UsageQueryFilter{Range: "1h", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &now}
			result, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Usage.TotalRequests != tc.want || result.Usage.TotalTokens != tc.want*10 {
				t.Fatalf("boundary leaked or lost events: %+v", result.Usage)
			}
			assertFloatClose(t, result.Summary.TotalCost, float64(tc.want)*0.5)
			comparison, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			model := comparison.Comparisons.Models["model"]
			if model == nil || model.Requests != tc.want || model.TotalTokens != tc.want*10 {
				t.Fatalf("wrong comparison boundary: %+v", comparison.Comparisons)
			}
		})
	}
}

func TestOverviewUTCFractionalRangeEdges(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "UTC")
	parse := func(value string) time.Time {
		at, err := time.Parse(time.RFC3339Nano, "2026-10-25T01:"+value+"Z")
		if err != nil {
			t.Fatal(err)
		}
		return at.In(location)
	}
	for _, tc := range []struct {
		name, start, end, event string
		closed                  bool
		want                    int64
	}{
		{"fraction after whole lower", "30:00", "31:00", "30:00.1", false, 1},
		{"whole before fractional lower", "30:00.5", "31:00", "30:00", false, 0},
		{"fraction before lower", "30:00.5", "31:00", "30:00.4", false, 0},
		{"exact fractional lower", "30:00.5", "31:00", "30:00.5", false, 1},
		{"fraction after whole upper", "30:00", "31:00", "31:00.1", false, 0},
		{"exact open upper", "30:00", "31:00", "31:00", false, 0},
		{"exact closed upper", "30:00", "31:00", "31:00", true, 1},
		{"whole before fractional upper", "30:00", "31:00.5", "31:00", false, 1},
		{"exact fractional open upper", "30:00", "31:00.5", "31:00.5", false, 0},
		{"exact fractional closed upper", "30:00", "31:00.5", "31:00.5", true, 1},
		{"fraction after closed upper", "30:00", "31:00.5", "31:00.6", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			cost, available := 0.5, true
			if err := db.Create(&entities.UsageEvent{EventKey: "edge", Timestamp: parse(tc.event), Model: "model", TotalTokens: 10, CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
				t.Fatal(err)
			}
			start, end := parse(tc.start), parse(tc.end)
			now := end.Add(48 * time.Hour)
			filter := dto.UsageQueryFilter{Range: "1h", StartTime: &start, EndTime: &end, EndExclusive: !tc.closed, QueryNow: &now}
			result, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Usage.TotalRequests != tc.want {
				t.Fatalf("want %d, got %+v", tc.want, result.Usage)
			}
			comparison, err := repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			model := comparison.Comparisons.Models["model"]
			if tc.want == 0 {
				if model != nil && model.Requests != 0 {
					t.Fatalf("unexpected comparison: %+v", model)
				}
			} else if model == nil || model.Requests != tc.want {
				t.Fatalf("wrong comparison: %+v", model)
			}
		})
	}
}

func TestRealtimeLondonFractionalTransitionDatabaseFallback(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "Europe/London")
	end, _ := time.Parse(time.RFC3339Nano, "2026-10-25T01:05:00Z")
	end = end.In(location)
	db := openTestDatabase(t)
	cost, available := 0.5, true
	for index, value := range []string{"2026-10-25T01:00:00Z", "2026-10-25T01:00:00.1Z", "2026-10-25T01:00:00.999999999Z"} {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entities.UsageEvent{EventKey: fmt.Sprintf("realtime-%d", index), Timestamp: at.In(location), Model: "model", TotalTokens: 10, CostUSD: &cost, CostAvailable: &available}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{RealtimeWindow: "60m", RealtimeEndTime: &end}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Insights.Summary.Requests != 3 || result.Insights.Summary.TotalTokens != 30 {
		t.Fatalf("lost or duplicated transition events: %+v", result.Insights.Summary)
	}
}
