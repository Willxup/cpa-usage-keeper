package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// 总览把完整小时的已存汇总与两端事件金额各计一次，不受当前模型报价影响。
func TestUsageOverviewCombinesStoredBucketAndBoundaryCosts(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	upsertUsageCostResolverPrice(t, db, "priced-model", 90)
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	fee := func(value float64) *float64 { return &value }
	available := func(value bool) *bool { return &value }
	events := []entities.UsageEvent{
		{EventKey: "left", APIGroupKey: "selected", Model: "priced-model", Timestamp: base.Add(-15 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(2), CostAvailable: available(true)},
		{EventKey: "inside-stats", APIGroupKey: "selected", Model: "priced-model", Timestamp: base.Add(15 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(300), CostAvailable: available(true)},
		{EventKey: "right", APIGroupKey: "selected", Model: "priced-model", Timestamp: base.Add(75 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(4), CostAvailable: available(true)},
		{EventKey: "other-key", APIGroupKey: "other", Model: "priced-model", Timestamp: base.Add(80 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(999), CostAvailable: available(true)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert boundary events: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: base, APIGroupKey: "selected", Model: "priced-model", RequestCount: 1, SuccessCount: 1,
		InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(3), UnavailableCostCount: func() *int64 { value := int64(0); return &value }(),
	}).Error; err != nil {
		t.Fatalf("insert hourly bucket: %v", err)
	}
	start, end := base.Add(-30*time.Minute), base.Add(90*time.Minute)
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		Range: "4h", StartTime: &start, EndTime: &end, APIGroupKey: "selected",
	}, nil)
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if !finiteStoredOverviewTestCost(overview.Summary.TotalCost, 9) || !overview.Summary.CostAvailable {
		t.Fatalf("stored summary fee mismatch: %+v", overview.Summary)
	}
	if overview.Usage.TotalRequests != 3 {
		t.Fatalf("full hour and two boundaries must each count once: %+v", overview.Usage)
	}
	for bucket, expected := range map[string]float64{
		timeutil.FormatStorageTime(base.Add(-time.Hour)): 2,
		timeutil.FormatStorageTime(base):                 3,
		timeutil.FormatStorageTime(base.Add(time.Hour)):  4,
	} {
		if !finiteStoredOverviewTestCost(overview.Series.Cost[bucket], expected) {
			t.Fatalf("bucket %s cost=%g, want %g", bucket, overview.Series.Cost[bucket], expected)
		}
	}
}

// 实时缓存与数据库回退都取事件已存金额；失败和零 Token 仍只进入请求计数。
func TestUsageRealtimeUsesStoredCostAcrossCacheAndDatabase(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	upsertUsageCostResolverPrice(t, db, "priced-model", 90)
	end := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	fee := func(value float64) *float64 { return &value }
	available := func(value bool) *bool { return &value }
	events := []entities.UsageEvent{
		{EventKey: "priced", APIGroupKey: "selected", Model: "priced-model", Timestamp: end.Add(-time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(7), CostAvailable: available(true)},
		{EventKey: "failed", APIGroupKey: "selected", Model: "priced-model", Timestamp: end.Add(-2 * time.Minute), Failed: true, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: fee(11), CostAvailable: available(true)},
		{EventKey: "no-token", APIGroupKey: "selected", Model: "priced-model", Timestamp: end.Add(-3 * time.Minute), CostUSD: fee(13), CostAvailable: available(true)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert realtime events: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
	if err != nil {
		t.Fatalf("load recent cache: %v", err)
	}
	t.Cleanup(cache.Close)
	filter := repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end, APIGroupKey: "selected"}
	for name, source := range map[string]*repository.UsageRecentEventCache{"cache": cache, "database": nil} {
		t.Run(name, func(t *testing.T) {
			result, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, source)
			if err != nil {
				t.Fatalf("build realtime: %v", err)
			}
			if len(result.CurrentUsage.Models) != 1 || result.CurrentUsage.Models[0].CostUSD == nil || !finiteStoredOverviewTestCost(*result.CurrentUsage.Models[0].CostUSD, 7) {
				t.Fatalf("current top must use stored successful token cost: %+v", result.CurrentUsage.Models)
			}
			var peakRequests int64
			for _, bucket := range result.RequestLevel {
				if bucket.Requests > peakRequests {
					peakRequests = bucket.Requests
				}
			}
			if peakRequests != 3 || result.CurrentUsage.Models[0].Requests != 3 {
				t.Fatalf("failed and zero-token events must still count as requests, peak=%d top=%+v", peakRequests, result.CurrentUsage.Models[0])
			}
		})
	}
}

// 同一完整桶只要有一行旧费用仍为 NULL，SUM 不能把该行悄悄忽略。
func TestUsageOverviewRejectsPartiallyUnbackfilledBucket(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	bucket := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	one, zeroUnavailable := 1.0, int64(0)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "selected", Model: "ready", RequestCount: 1, CostUSD: &one, UnavailableCostCount: &zeroUnavailable},
		{BucketStart: bucket, APIGroupKey: "selected", Model: "old-null", RequestCount: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed mixed bucket: %v", err)
	}
	end := bucket.Add(time.Hour)
	if _, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &bucket, EndTime: &end, EndExclusive: true, APIGroupKey: "selected",
	}, nil); err == nil {
		t.Fatal("partially unbackfilled bucket was accepted")
	}
}

// 精确边界和实时有效事件拒绝 NULL 费用，但显式缺价零值能保留不可用状态。
func TestUsageOverviewBoundaryAndRealtimeDistinguishNullAndUnavailableCost(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	for _, test := range []struct {
		name      string
		cost      *float64
		available *bool
		wantError bool
	}{
		{name: "missing amount", available: overviewAvailabilityPtr(true), wantError: true},
		{name: "missing availability", cost: overviewCostPtr(0), wantError: true},
		{name: "known unavailable", cost: overviewCostPtr(0), available: overviewAvailabilityPtr(false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDatabase(t)
			end := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
			event := entities.UsageEvent{EventKey: test.name, APIGroupKey: "selected", Model: "unpriced", Timestamp: end.Add(-time.Minute), InputTokens: 10, TotalTokens: 10, CostUSD: test.cost, CostAvailable: test.available}
			if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
				t.Fatalf("seed unbackfilled event: %v", err)
			}
			start := end.Add(-15 * time.Minute)
			overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(db, repodto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end, APIGroupKey: "selected"}, nil)
			if test.wantError {
				if err == nil {
					t.Fatal("boundary accepted unbackfilled fee")
				}
			} else if err != nil || !finiteStoredOverviewTestCost(overview.Summary.TotalCost, 0) || overview.Summary.CostAvailable {
				t.Fatalf("boundary changed explicit unavailable zero: overview=%+v err=%v", overview, err)
			}
			realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, repodto.UsageQueryFilter{RealtimeWindow: "15m", RealtimeEndTime: &end, APIGroupKey: "selected"}, nil)
			if test.wantError {
				if err == nil {
					t.Fatal("realtime accepted unbackfilled fee")
				}
			} else if err != nil || !finiteStoredOverviewTestCost(realtime.Insights.Summary.CostUSD, 0) || realtime.Insights.Summary.CostAvailable {
				t.Fatalf("realtime changed explicit unavailable zero: realtime=%+v err=%v", realtime.Insights.Summary, err)
			}
		})
	}
}

func finiteStoredOverviewTestCost(got, want float64) bool {
	return !math.IsNaN(got) && !math.IsInf(got, 0) && math.Abs(got-want) <= 1e-12
}
