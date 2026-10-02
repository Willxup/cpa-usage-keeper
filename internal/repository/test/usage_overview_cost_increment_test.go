package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// TestUsageOverviewIncrementUsesStoredEventFees 验证普通聚合只累加事件已存费用与缺价数。
func TestUsageOverviewIncrementUsesStoredEventFees(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	events := []entities.UsageEvent{
		{EventKey: "priced", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-50 * time.Minute), InputTokens: 100, TotalTokens: 100, CostUSD: overviewCostPtr(1.25), CostAvailable: overviewAvailabilityPtr(true)},
		{EventKey: "missing-price", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-30 * time.Minute), InputTokens: 20, TotalTokens: 20, CostUSD: overviewCostPtr(0), CostAvailable: overviewAvailabilityPtr(false)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate first stored fees: %v", err)
	}
	assertOverviewStoredFeeBuckets(t, db, 1.25, 1, 2, 120)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "later", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-time.Minute), InputTokens: 5, TotalTokens: 5, CostUSD: overviewCostPtr(2), CostAvailable: overviewAvailabilityPtr(true)}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatalf("aggregate incremental stored fee: %v", err)
	}
	assertOverviewStoredFeeBuckets(t, db, 3.25, 1, 3, 125)
	assertUsageOverviewCheckpoint(t, db, 3)
}

// TestUsageOverviewIncrementRejectsUnbackfilledEvent 证明未填费用不会借零值推进 Overview 水位。
func TestUsageOverviewIncrementRejectsUnbackfilledEvent(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "old-null", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-time.Minute), InputTokens: 10}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err == nil || !strings.Contains(err.Error(), "not backfilled") {
		t.Fatalf("unbackfilled event accepted: %v", err)
	}
	assertOverviewNoCommittedRows(t, db)
}

// TestUsageOverviewIncrementRejectsUnbackfilledBucket 证明已有 NULL 桶不能被新差额静默污染。
func TestUsageOverviewIncrementRejectsUnbackfilledBucket(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	if err := db.Create(&entities.UsageOverviewHourlyStat{BucketStart: now.Add(-time.Hour), APIGroupKey: "key-a", Model: "model-a", RequestCount: 7, TotalTokens: 70, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "new-priced", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-time.Minute), InputTokens: 10, TotalTokens: 10, CostUSD: overviewCostPtr(2), CostAvailable: overviewAvailabilityPtr(true)}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err == nil || !strings.Contains(err.Error(), "not backfilled") {
		t.Fatalf("unbackfilled bucket accepted: %v", err)
	}
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("api_group_key = ?", "key-a").Take(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	if hourly.CostUSD != nil || hourly.RequestCount != 7 || hourly.TotalTokens != 70 {
		t.Fatalf("NULL bucket was changed: %+v", hourly)
	}
	var dailyCount, cursorCount int64
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Count(&dailyCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).Count(&cursorCount).Error; err != nil {
		t.Fatal(err)
	}
	if dailyCount != 0 || cursorCount != 0 {
		t.Fatalf("NULL bucket failure advanced daily/checkpoint: daily=%d checkpoint=%d", dailyCount, cursorCount)
	}
}

// TestUsageOverviewFeeRollsBackWithDailyFailure 验证小时费用、日费用和水位在日写失败时共同回滚。
func TestUsageOverviewFeeRollsBackWithDailyFailure(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "daily-fails", APIGroupKey: "key-a", Model: "model-a", Timestamp: now.Add(-time.Minute), CostUSD: overviewCostPtr(3), CostAvailable: overviewAvailabilityPtr(true)}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_fee_daily BEFORE INSERT ON usage_overview_daily_stats BEGIN SELECT RAISE(ABORT, 'daily fee failed'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err == nil {
		t.Fatal("expected daily fee write failure")
	}
	assertOverviewNoCommittedRows(t, db)
	if err := db.Exec(`DROP TRIGGER fail_fee_daily`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("retry after daily failure: %v", err)
	}
	assertOverviewStoredFeeBuckets(t, db, 3, 0, 1, 0)
	assertUsageOverviewCheckpoint(t, db, 1)
}

// TestUsageOverviewRejectsNonFiniteAccumulatedBucketCost 验证两个有限事件之和溢出时整批回滚。
func TestUsageOverviewRejectsNonFiniteAccumulatedBucketCost(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		secondAtHour int
	}{
		{name: "hourly overflow", secondAtHour: 10},
		{name: "daily overflow", secondAtHour: 11},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
			piece := math.MaxFloat64 * 0.75
			first := entities.UsageEvent{EventKey: "first-large", APIGroupKey: "key-a", Model: "model-a", Timestamp: time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local), CostUSD: overviewCostPtr(piece), CostAvailable: overviewAvailabilityPtr(true)}
			if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{first}); err != nil {
				t.Fatal(err)
			}
			if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
				t.Fatalf("first finite event: %v", err)
			}
			second := entities.UsageEvent{EventKey: "second-large", APIGroupKey: "key-a", Model: "model-a", Timestamp: time.Date(2026, 9, 23, testCase.secondAtHour, 30, 0, 0, time.Local), CostUSD: overviewCostPtr(piece), CostAvailable: overviewAvailabilityPtr(true)}
			if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{second}); err != nil {
				t.Fatal(err)
			}
			if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err == nil || !strings.Contains(err.Error(), "not finite") {
				t.Fatalf("finite event fees overflowed a persisted bucket without a finite-value error: %v", err)
			}
			assertUsageOverviewCheckpoint(t, db, 1)
			var hourly []entities.UsageOverviewHourlyStat
			if err := db.Find(&hourly).Error; err != nil || len(hourly) != 1 {
				t.Fatalf("failed batch changed hourly buckets: rows=%+v err=%v", hourly, err)
			}
			var daily []entities.UsageOverviewDailyStat
			if err := db.Find(&daily).Error; err != nil || len(daily) != 1 {
				t.Fatalf("failed batch changed daily buckets: rows=%+v err=%v", daily, err)
			}
			if hourly[0].CostUSD == nil || daily[0].CostUSD == nil || *hourly[0].CostUSD != piece || *daily[0].CostUSD != piece || hourly[0].RequestCount != 1 || daily[0].RequestCount != 1 {
				t.Fatalf("overflow batch left partial or non-finite bucket: hourly=%+v daily=%+v", hourly[0], daily[0])
			}
		})
	}
}

func assertOverviewNoCommittedRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range []any{&entities.UsageOverviewHourlyStat{}, &entities.UsageOverviewDailyStat{}, &entities.UsageAggregationCheckpoint{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed overview page left rows for %T: count=%d err=%v", model, count, err)
		}
	}
}

func assertOverviewStoredFeeBuckets(t *testing.T, db *gorm.DB, wantCost float64, wantUnavailable, wantRequests, wantTokens int64) {
	t.Helper()
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("api_group_key = ? AND model = ?", "key-a", "model-a").Take(&hourly).Error; err != nil {
		t.Fatalf("load hourly fee bucket: %v", err)
	}
	var daily entities.UsageOverviewDailyStat
	if err := db.Where("api_group_key = ? AND model = ?", "key-a", "model-a").Take(&daily).Error; err != nil {
		t.Fatalf("load daily fee bucket: %v", err)
	}
	for grain, values := range map[string]struct {
		cost             *float64
		unavailable      *int64
		requests, tokens int64
	}{
		"hourly": {hourly.CostUSD, hourly.UnavailableCostCount, hourly.RequestCount, hourly.TotalTokens},
		"daily":  {daily.CostUSD, daily.UnavailableCostCount, daily.RequestCount, daily.TotalTokens},
	} {
		if values.cost == nil || values.unavailable == nil || math.IsNaN(*values.cost) || math.IsInf(*values.cost, 0) || math.Abs(*values.cost-wantCost) > 1e-9 || *values.unavailable != wantUnavailable || values.requests != wantRequests || values.tokens != wantTokens {
			t.Fatalf("%s fee bucket = %+v, want cost=%v unavailable=%d requests=%d tokens=%d", grain, values, wantCost, wantUnavailable, wantRequests, wantTokens)
		}
	}
}

func overviewCostPtr(value float64) *float64   { return &value }
func overviewAvailabilityPtr(value bool) *bool { return &value }

// priceOverviewFixtureEvents 仅供明确要模拟正常聚合的测试，在插入前按当时已存价格填费用。
// 历史迁移与 NULL 门禁测试不调用它，避免把待回填旧事件伪装为零费用。
func priceOverviewFixtureEvents(t *testing.T, db *gorm.DB, events []entities.UsageEvent) []entities.UsageEvent {
	t.Helper()
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load fixture pricing snapshot: %v", err)
	}
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	for index := range events {
		if events[index].CostUSD != nil || events[index].CostAvailable != nil {
			if events[index].CostUSD == nil || events[index].CostAvailable == nil {
				t.Fatalf("fixture event %q has incomplete stored fee", events[index].EventKey)
			}
			continue
		}
		fee := resolver.CalculateFee(repository.UsageEventCostSubject(events[index]))
		events[index].CostUSD = overviewCostPtr(fee.TotalCostUSD)
		events[index].CostAvailable = overviewAvailabilityPtr(fee.Available)
	}
	return events
}
