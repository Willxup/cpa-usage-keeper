package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestAnalysisCalendarDayExcludesAdjacentDates(t *testing.T) {
	for _, tc := range []struct {
		zone        string
		month       time.Month
		day         int
		granularity dto.AnalysisGranularity
	}{
		{"UTC", time.April, 5, dto.AnalysisGranularityHourly},
		{"Asia/Shanghai", time.April, 5, dto.AnalysisGranularityHourly},
		{"America/Los_Angeles", time.March, 8, dto.AnalysisGranularityHourly},
		{"America/Los_Angeles", time.November, 1, dto.AnalysisGranularityHourly},
		{"Australia/Lord_Howe", time.April, 5, dto.AnalysisGranularityDaily},
		{"Australia/Adelaide", time.April, 5, dto.AnalysisGranularityDaily},
		{"Pacific/Chatham", time.April, 5, dto.AnalysisGranularityDaily},
	} {
		for _, rangeName := range []string{"today", "yesterday"} {
			t.Run(tc.zone+"/"+tc.month.String()+"/"+rangeName, func(t *testing.T) {
				location := setAnalysisLatencyTestTimezone(t, tc.zone)
				start := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, location)
				next := time.Date(2026, tc.month, tc.day+1, 0, 0, 0, 0, location)
				anchor := time.Date(2026, tc.month, tc.day, 12, 0, 0, 0, location)
				if rangeName == "yesterday" {
					anchor = anchor.AddDate(0, 0, 1)
				}
				parsed, err := timeutil.ParseUsageQueryRange(rangeName, "", "", "", anchor)
				if err != nil {
					t.Fatal(err)
				}
				db := openTestDatabase(t)
				if err := db.Create(&entities.CPAAPIKey{APIKey: "boundary-key"}).Error; err != nil {
					t.Fatal(err)
				}
				var events []entities.UsageEvent
				for _, event := range []struct {
					key    string
					at     time.Time
					tokens int64
				}{
					{"previous-day", start.Add(-5 * time.Minute), 1000},
					{"first-in-day", start.Add(5 * time.Minute), 100},
					{"last-in-day", next.Add(-5 * time.Minute), 200},
					{"next-day", next.Add(5 * time.Minute), 9000},
				} {
					events = append(events, entities.UsageEvent{
						EventKey: event.key, APIGroupKey: "boundary-key", Model: "model", Timestamp: event.at,
						TotalTokens: event.tokens, OutputTokens: event.tokens, LatencyMS: 1000,
						TTFTMS: new(int64(100)), Generate: new(true), CostAvailable: new(true), CostUSD: new(float64(event.tokens) / 100),
					})
				}
				if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
					t.Fatal(err)
				}
				if err := repository.AggregateUsageOverviewStats(context.Background(), db, next.Add(12*time.Hour)); err != nil {
					t.Fatal(err)
				}
				// 原始事件可已过保留期，必须仅凭现有增量表隔离相邻日期。
				if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
					t.Fatal(err)
				}
				filter := dto.UsageQueryFilter{Range: rangeName, StartTime: &parsed.StartTime, EndTime: &parsed.EndTime, QueryNow: &anchor, APIGroupKey: "boundary-key"}
				queries := captureAllAnalysisQueries(t, db)
				got, err := repository.BuildAnalysisWithFilter(db, filter)
				if err != nil {
					t.Fatal(err)
				}
				var tokens, requests int64
				for _, bucket := range got.TokenUsage {
					tokens += bucket.TotalTokens
					requests += bucket.Requests
				}
				if tokens != 300 || requests != 2 || !got.CostSummary.CostAvailable {
					t.Errorf("adjacent dates affected totals: tokens=%d requests=%d cost=%+v", tokens, requests, got.CostSummary)
				}
				assertFloatClose(t, got.CostSummary.TotalCostUSD, 3)
				if len(got.ModelSpeed.Series) != 1 || got.ModelSpeed.Series[0].TotalTokens != 300 || got.ModelSpeed.Series[0].SpeedSampleCount != 2 {
					t.Fatalf("adjacent dates affected model speed: %+v", got.ModelSpeed.Series)
				}
				var speedSum float64
				for _, sample := range got.ModelSpeed.Series[0].Samples {
					speedSum += sample.SpeedTPSSum
				}
				assertFloatClose(t, speedSum/float64(got.ModelSpeed.Series[0].SpeedSampleCount), 150)
				if got.Granularity != tc.granularity {
					t.Fatalf("granularity = %s, want %s", got.Granularity, tc.granularity)
				}
				if tc.granularity == dto.AnalysisGranularityDaily {
					requireSingleAnalysisRollupQuery(t, *queries, "usage_overview_daily_stats")
					// 退回已存在的日汇总契约，完整响应保持与显式日粒度一致。
					filter.Range, filter.CustomUnit = "custom", "day"
					want, err := repository.BuildAnalysisWithFilter(db, filter)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatal("calendar-day response differs from explicit daily response")
					}
				} else {
					requireSingleAnalysisRollupQuery(t, *queries, "usage_overview_hourly_stats")
					if len(got.TokenUsage) != int(next.Sub(start)/time.Hour)+1 {
						t.Fatalf("incomplete hourly axis: %+v", got.TokenUsage)
					}
					last := got.TokenUsage[len(got.TokenUsage)-1]
					if !last.Bucket.Equal(next) || last.TotalTokens != 0 || last.Requests != 0 {
						t.Fatalf("missing empty next-midnight sentinel: %+v", last)
					}
				}
			})
		}
	}
}

func TestAnalysisCalendarDayKeepsHourlyDataAndEndingSentinel(t *testing.T) {
	for _, tc := range []struct {
		zone       string
		month      time.Month
		day, hours int
	}{
		{"Asia/Shanghai", 3, 8, 24},
		{"America/Los_Angeles", 3, 8, 23},
		{"America/Los_Angeles", 11, 1, 25},
		{"Europe/Berlin", 10, 25, 25},
		{"America/Santiago", 9, 6, 23},
	} {
		t.Run(tc.zone+"/"+tc.month.String(), func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			anchor := time.Date(2026, tc.month, tc.day, 12, 0, 0, 0, location)
			rangeValue, err := timeutil.ParseUsageQueryRange("today", "", "", "", anchor)
			if err != nil {
				t.Fatal(err)
			}
			start, end := rangeValue.StartTime, rangeValue.EndTime
			db := openTestDatabase(t)
			if err := db.Create(&entities.CPAAPIKey{APIKey: "calendar-key"}).Error; err != nil {
				t.Fatal(err)
			}
			cost, unavailable := 0.0, int64(0)
			var hourly []entities.UsageOverviewHourlyStat
			var events []entities.UsageEvent
			for i := 0; i < tc.hours; i++ {
				at := start.Add(time.Duration(i) * time.Hour)
				hourly = append(hourly, entities.UsageOverviewHourlyStat{BucketStart: at, APIGroupKey: "calendar-key", Model: "model", RequestCount: 1, TotalTokens: 100, CostUSD: &cost, UnavailableCostCount: &unavailable})
				events = append(events, validLatencyEvent(int64(i+1), "calendar-key", at.Add(time.Minute), int64(i+1), int64(i+1)*10))
			}
			if err := db.Create(&hourly).Error; err != nil {
				t.Fatal(err)
			}
			seedAnalysisLatencyRows(t, db, end, entities.UsageLatencyBucketHour, events)
			filter := dto.UsageQueryFilter{Range: "today", StartTime: &start, EndTime: &end}
			analysis, err := repository.BuildAnalysisWithFilter(db, filter)
			if err != nil {
				t.Fatal(err)
			}
			if analysis.Granularity != dto.AnalysisGranularityHourly || len(analysis.TokenUsage) != tc.hours+1 {
				t.Fatalf("want %d hourly rows including sentinel, got %s/%d", tc.hours+1, analysis.Granularity, len(analysis.TokenUsage))
			}
			for i, point := range analysis.TokenUsage {
				want := int64(100)
				if i == tc.hours {
					want = 0
				}
				if point.TotalTokens != want || !point.Bucket.Equal(start.Add(time.Duration(i)*time.Hour)) {
					t.Fatalf("wrong point[%d]: %+v", i, point)
				}
			}
			diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, filter)
			if err != nil {
				t.Fatal(err)
			}
			if diagnostics.TotalPoints != int64(tc.hours) {
				t.Fatalf("lost hourly diagnostics: got %d, want %d", diagnostics.TotalPoints, tc.hours)
			}
		})
	}
}

func TestAnalysisLatencyReadsExistingCalendarAndLocalHourBuckets(t *testing.T) {
	for _, tc := range []struct{ zone, start, end, unit string }{
		{"America/Santiago", "2026-09-06T01:00:00-03:00", "2026-09-07T00:00:00-03:00", "day"},
		{"Europe/Berlin", "2026-10-25T02:00:00+02:00", "2026-10-25T02:00:00+01:00", "hour"},
		{"Asia/Kathmandu", "2026-10-25T02:00:00+05:45", "2026-10-25T03:00:00+05:45", "hour"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			start, err := time.Parse(time.RFC3339, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			end, err := time.Parse(time.RFC3339, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			start, end = start.In(location), end.In(location)
			db := openTestDatabase(t)
			kind := entities.UsageLatencyBucketHour
			if tc.unit == "day" {
				kind = entities.UsageLatencyBucketDay
			}
			seedAnalysisLatencyRows(t, db, end.Add(time.Hour), kind, []entities.UsageEvent{
				validLatencyEvent(1, "key", start.Add(time.Minute), 100, 1000),
				validLatencyEvent(2, "key", end.Add(time.Minute), 200, 2000),
			})
			diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, dto.UsageQueryFilter{Range: "custom", CustomUnit: tc.unit, StartTime: &start, EndTime: &end, EndExclusive: true})
			if err != nil {
				t.Fatal(err)
			}
			assertSingleAnalysisLatencyPoint(t, diagnostics, 100, 1000)
		})
	}
}
