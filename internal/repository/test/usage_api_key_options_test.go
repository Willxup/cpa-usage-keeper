package test

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestUsageAPIKeyOptionsUsePageSpecificSources(t *testing.T) {
	setAnalysisLatencyTestTimezone(t, "Asia/Shanghai")
	db := openTestDatabase(t)
	now := time.Date(2026, 5, 20, 12, 23, 0, 0, time.Local)
	start := now.AddDate(0, 0, -7)
	all := []string{"left", "hour-left", "daily", "right", "raw-interior", "hour-recent", "hour-current", "day-left", "day-right", "cached", "future", "unused", "deleted"}
	if err := repository.SyncCPAAPIKeys(db, all, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.CPAAPIKey{}).Where("api_key = ?", "deleted").Update("is_deleted", true).Error; err != nil {
		t.Fatal(err)
	}
	event := func(key string, at time.Time) entities.UsageEvent {
		cost, available := 0.0, true
		row := entities.UsageEvent{EventKey: key, APIGroupKey: key, Timestamp: at, CostUSD: &cost, CostAvailable: &available}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	event("left", start.Add(time.Minute))
	event("right", now.Add(-time.Minute))
	event("raw-interior", now.AddDate(0, 0, -3))
	event("deleted", now.Add(-time.Minute))
	hourly := func(key string, at time.Time) {
		if err := db.Create(&entities.UsageOverviewHourlyStat{APIGroupKey: key, BucketStart: at, RequestCount: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	daily := func(key string, at time.Time) {
		if err := db.Create(&entities.UsageOverviewDailyStat{APIGroupKey: key, BucketStart: at, RequestCount: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	hourly("hour-left", start.Truncate(time.Hour).Add(2*time.Hour))
	hourly("hour-recent", now.Truncate(time.Hour).Add(-2*time.Hour))
	hourly("hour-current", now.Truncate(time.Hour))
	daily("daily", time.Date(2026, 5, 16, 0, 0, 0, 0, time.Local))
	daily("day-left", time.Date(2026, 5, 13, 0, 0, 0, 0, time.Local))
	daily("day-right", time.Date(2026, 5, 20, 0, 0, 0, 0, time.Local))
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	// 缓存独有的新事件不要求先出现在 DB；当前右边界保留开放上界。
	if !cache.TryAppend([]entities.UsageEvent{{APIGroupKey: "cached", Timestamp: now.Add(-time.Minute)}, {APIGroupKey: "future", Timestamp: now.Add(time.Minute)}}) {
		t.Fatal("append failed")
	}
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	base := dto.UsageQueryFilter{Range: "7d", StartTime: &start, EndTime: &now, EndExclusive: true, QueryNow: &now, APIGroupKey: "unused", Model: "missing", Result: "failed"}
	check := func(name, scope string, filter dto.UsageQueryFilter, c *repository.UsageRecentEventCache, want ...string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			rows, err := repository.ListUsageAPIKeyOptions(db, scope, filter, c)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, r := range rows {
				got = append(got, r.APIKey)
			}
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
	check("overview-long", "overview", base, cache, "left", "hour-left", "daily", "hour-recent", "right", "cached", "future")
	check("overview-fallback", "overview", base, nil, "left", "hour-left", "daily", "hour-recent", "right")
	check("analysis-calendar-days", "analysis", base, cache, "daily", "day-left", "day-right")
	check("events-ignore-other-filters", "request-events", base, cache, "left", "right", "raw-interior")
	check("realtime-cache", "realtime", dto.UsageQueryFilter{RealtimeWindow: "15m", QueryNow: &now}, cache, "right", "cached")
	check("realtime-fallback", "realtime", dto.UsageQueryFilter{RealtimeWindow: "15m", QueryNow: &now}, nil, "right")
	hs, he := now.Truncate(time.Hour).Add(-3*time.Hour), now.Truncate(time.Hour).Add(time.Hour)
	custom := dto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &hs, EndTime: &he, EndExclusive: true, QueryNow: &now}
	check("overview-custom-hour", "overview", custom, cache, "hour-recent", "hour-current")
	check("analysis-custom-hour", "analysis", custom, cache, "hour-recent", "hour-current")
	rollingStart := now.Add(-8 * time.Hour)
	rolling := dto.UsageQueryFilter{Range: "8h", StartTime: &rollingStart, EndTime: &now, EndExclusive: true, QueryNow: &now}
	check("overview-short", "overview", rolling, cache, "hour-recent", "right", "cached", "future")
	check("analysis-incremental-hour", "analysis", rolling, nil, "hour-recent", "hour-current")
	ds, de := time.Date(2026, 5, 13, 0, 0, 0, 0, time.Local), time.Date(2026, 5, 21, 0, 0, 0, 0, time.Local)
	custom = dto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &ds, EndTime: &de, EndExclusive: true, QueryNow: &now}
	if err := db.Where("1 = 1").Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatal(err)
	}
	check("overview-retained-summary", "overview", custom, nil, "daily", "day-left", "day-right")
	check("analysis-retained-summary", "analysis", custom, nil, "daily", "day-left", "day-right")
}

func TestUsageAPIKeyOptionsExactEventBoundaries(t *testing.T) {
	for _, tc := range []struct{ zone, start, end string }{
		{"UTC", "2026-05-20T10:00:00.5Z", "2026-05-20T10:00:01Z"},
		{"Asia/Shanghai", "2026-05-20T10:00:00.5+08:00", "2026-05-20T10:00:01+08:00"},
		{"America/New_York", "2026-11-01T01:00:00-05:00", "2026-11-01T02:00:00-05:00"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			setAnalysisLatencyTestTimezone(t, tc.zone)
			db := openTestDatabase(t)
			start, _ := time.Parse(time.RFC3339Nano, tc.start)
			end, _ := time.Parse(time.RFC3339Nano, tc.end)
			if err := repository.SyncCPAAPIKeys(db, []string{"before", "start", "inside", "end"}, start); err != nil {
				t.Fatal(err)
			}
			for i, at := range []time.Time{start.Add(-time.Nanosecond), start, start.Add(time.Nanosecond), end} {
				key := []string{"before", "start", "inside", "end"}[i]
				if err := db.Create(&entities.UsageEvent{EventKey: key, APIGroupKey: key, Timestamp: at}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, exclusive := range []bool{true, false} {
				f := dto.UsageQueryFilter{StartTime: &start, EndTime: &end, EndExclusive: exclusive}
				rows, err := repository.ListUsageAPIKeyOptions(db, "request-events", f, nil)
				if err != nil {
					t.Fatal(err)
				}
				want := 2
				if !exclusive {
					want = 3
				}
				if len(rows) != want {
					t.Fatalf("exclusive=%t got %+v", exclusive, rows)
				}
			}
		})
	}
}

func TestUsageAPIKeyOptionsCacheCopiesOnlyKeys(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Now()
	c, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 2; i++ {
		if !c.TryAppend([]entities.UsageEvent{{APIGroupKey: "used", Timestamp: now.Add(-time.Minute)}, {APIGroupKey: "future", Timestamp: now.Add(time.Minute)}}) {
			t.Fatal("append")
		}
	}
	if err := c.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	keys, ok := c.UsedAPIKeys(now.Add(-time.Hour), now, false, false)
	if !ok || len(keys) != 1 {
		t.Fatalf("keys=%v ok=%t", keys, ok)
	}
	delete(keys, "used")
	keys["foreign"] = struct{}{}
	again, ok := c.UsedAPIKeys(now.Add(-time.Hour), now, false, true)
	if !ok || len(again) != 2 {
		t.Fatal(again)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.ListUsageAPIKeyOptions(db.WithContext(ctx), "realtime", dto.UsageQueryFilter{}, c); err == nil {
		t.Fatal("canceled request succeeded")
	}
	if _, err := repository.ListUsageAPIKeyOptions(db, "invalid", dto.UsageQueryFilter{}, nil); err == nil {
		t.Fatal("invalid scope succeeded")
	}
}
