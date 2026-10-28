package test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestRecentEventTimeReadsAgreeAcrossCacheLifecycles(t *testing.T) {
	for _, tc := range []struct{ zone, now string }{
		{"Europe/London", "2026-10-25T01:30:00Z"},
		{"Europe/Berlin", "2026-10-25T02:30:00+01:00"},
		{"America/New_York", "2026-11-01T01:30:00-05:00"},
		{"Europe/London", "2026-03-29T02:30:00+01:00"},
		{"UTC", "2026-10-25T01:30:00.5Z"},
		{"Asia/Shanghai", "2026-10-25T01:30:00.5+08:00"},
	} {
		t.Run(tc.zone+tc.now, func(t *testing.T) {
			location := setAnalysisLatencyTestTimezone(t, tc.zone)
			now, err := time.Parse(time.RFC3339Nano, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			now = now.In(location)
			cutoff := now.Add(-70 * time.Minute)
			db := openTestDatabase(t)
			options := repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }}
			appended, err := repository.NewUsageRecentEventCache(db, options)
			if err != nil {
				t.Fatal(err)
			}
			defer appended.Close()
			var events []entities.UsageEvent
			for index, at := range []time.Time{cutoff.Add(-time.Nanosecond), cutoff, now.Add(-30 * time.Minute), now.Add(-30*time.Minute + time.Nanosecond), now.Add(-time.Minute), now.Add(10 * time.Minute)} {
				cost, available, ttft := 0.5, true, int64(100)
				event := entities.UsageEvent{EventKey: fmt.Sprintf("time-read-%d", index), Timestamp: at, APIGroupKey: "target", Model: "model", AuthType: "oauth", AuthIndex: "auth", Generate: &available, LatencyMS: 1000, TTFTMS: &ttft, InputTokens: 2, OutputTokens: 8, TotalTokens: 10, CostUSD: &cost, CostAvailable: &available}
				// 过期且费用未回填的数据不应令费用重载失败。
				if index == 0 {
					event.CostUSD, event.CostAvailable = nil, nil
				}
				if err := db.Create(&event).Error; err != nil {
					t.Fatal(err)
				}
				events = append(events, event)
			}
			other := events[2]
			other.ID, other.EventKey, other.APIGroupKey, other.TotalTokens = 0, "other-key", "other", 999
			if err := db.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			events = append(events, other)
			if !appended.TryAppend(events) {
				t.Fatal("append rejected")
			}
			if err := appended.DrainAcceptedAppends(context.Background()); err != nil {
				t.Fatal(err)
			}
			cold, err := repository.NewUsageRecentEventCache(db, options)
			if err != nil {
				t.Fatal(err)
			}
			defer cold.Close()
			filter := dto.UsageQueryFilter{RealtimeWindow: "60m", RealtimeEndTime: &now, APIGroupKey: " target "}
			expected, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			if expected.Insights.Summary.Requests != 3 || expected.Insights.Summary.TotalTokens != 30 {
				t.Fatalf("bad reference: %+v", expected.Insights.Summary)
			}
			check := func(name string, cache *repository.UsageRecentEventCache) {
				t.Helper()
				actual, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, cache)
				if err != nil {
					t.Fatal(err)
				}
				// Model Speed 按既有契约只由缓存提供，DB fallback 明确不可用。
				if !actual.ModelSpeed.Available || len(actual.ModelSpeed.Series) != 1 || actual.ModelSpeed.Series[0].SpeedSampleCount != 3 {
					t.Fatalf("%s wrong cached model speed: %+v", name, actual.ModelSpeed)
				}
				want := expected
				want.ModelSpeed = actual.ModelSpeed
				if !reflect.DeepEqual(actual, want) {
					a, b := reflect.ValueOf(actual), reflect.ValueOf(want)
					for i := 0; i < a.NumField(); i++ {
						if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
							av, _ := json.Marshal(a.Field(i).Interface())
							bv, _ := json.Marshal(b.Field(i).Interface())
							t.Errorf("%s field %s: got %s want %s", name, a.Type().Field(i).Name, av, bv)
						}
					}
					t.FailNow()
				}
				loaded, ok := cache.EventsSince(cutoff.Add(-time.Second), "target")
				if !ok || len(loaded) != 5 {
					t.Fatalf("%s retention: %d events, available=%t", name, len(loaded), ok)
				}
				future, ok := cache.EventsSince(now.Add(time.Minute), "target")
				if !ok || len(future) != 1 || !future[0].Timestamp.Equal(now.Add(10*time.Minute)) {
					t.Fatalf("%s lost open-ended future event: %+v", name, future)
				}
			}
			check("append", appended)
			check("cold start", cold)
			if err := cold.ReloadStoredCostEvents(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			check("reload", cold)
			if err := db.Model(&entities.UsageEvent{}).Where("api_group_key = ? AND cost_usd IS NOT NULL", "target").Update("cost_usd", 1.5).Error; err != nil {
				t.Fatal(err)
			}
			expected, err = repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := cold.ReloadStoredCostEvents(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			check("recalculated fees", cold)
		})
	}
}
