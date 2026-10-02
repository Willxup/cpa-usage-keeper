package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

// 短额度窗口只累计事件已存费用，不能按查询时的模型单价重算。
func TestUsageWindowShortRangeReadsStoredEventCost(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	upsertUsageCostResolverPrice(t, db, "model-a", 9)
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cost, available := 2.5, true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "stored-short", AuthIndex: "auth-a", Model: "model-a", Timestamp: start.Add(time.Minute),
		InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &cost, CostAvailable: &available,
	}}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil {
		t.Fatalf("sum short window: %v", err)
	}
	if !stats.CostAvailable || stats.Tokens != 1_000_000 || !finiteStoredOverviewTestCost(stats.Cost, cost) {
		t.Fatalf("short window repriced stored fee: %+v", stats)
	}
}

// 空库窗口与显式免费请求均有可靠零费用；缺价请求虽金额为零仍标记不可用。
func TestUsageWindowPreservesStoredFreeAndMissingPriceAvailability(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	calculator := newUsageWindowCalculatorForTest(t, db)
	empty, err := calculator.SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil || empty.Tokens != 0 || empty.Cost != 0 || !empty.CostAvailable {
		t.Fatalf("empty window should be a known zero: stats=%+v err=%v", empty, err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "free", AuthIndex: "auth-a", Model: "free-model", Timestamp: start.Add(time.Minute),
		InputTokens: 10, TotalTokens: 10, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(true),
	}}); err != nil {
		t.Fatalf("seed free event: %v", err)
	}
	free, err := calculator.SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil || free.Tokens != 10 || free.Cost != 0 || !free.CostAvailable {
		t.Fatalf("explicit free event lost availability: stats=%+v err=%v", free, err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "unpriced", AuthIndex: "auth-a", Model: "unpriced-model", Timestamp: start.Add(2 * time.Minute),
		InputTokens: 20, TotalTokens: 20, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(false),
	}}); err != nil {
		t.Fatalf("seed unpriced event: %v", err)
	}
	mixed, err := calculator.SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil || mixed.Tokens != 30 || mixed.Cost != 0 || mixed.CostAvailable {
		t.Fatalf("missing-price event was treated as free: stats=%+v err=%v", mixed, err)
	}
}

// raw 事件及完整小时汇总的金额或可用性任一列未回填时，窗口不能输出伪零值。
func TestUsageWindowRejectsUnbackfilledRawAndHourlyCosts(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	for _, source := range []string{"raw", "hourly"} {
		for _, missing := range []string{"cost", "availability"} {
			t.Run(source+"/"+missing, func(t *testing.T) {
				db := openTestDatabase(t)
				start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
				end := start.Add(time.Hour)
				cost, available, unavailable := windowCostPtr(1), windowAvailablePtr(true), windowUnavailablePtr(0)
				if missing == "cost" {
					cost = nil
				} else {
					available, unavailable = nil, nil
				}
				if source == "raw" {
					if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
						EventKey: "null-raw", AuthIndex: "auth-a", Model: "model-a", Timestamp: start.Add(time.Minute),
						TotalTokens: 10, CostUSD: cost, CostAvailable: available,
					}}); err != nil {
						t.Fatalf("seed raw row: %v", err)
					}
				} else {
					end = start.Add(7 * 24 * time.Hour)
					if err := db.Create(&entities.UsageOverviewHourlyStat{
						BucketStart: start.Add(24 * time.Hour), AuthIndex: "auth-a", Model: "model-a", TotalTokens: 10,
						CostUSD: cost, UnavailableCostCount: unavailable,
					}).Error; err != nil {
						t.Fatalf("seed hourly row: %v", err)
					}
				}
				if _, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-a", start, &end); err == nil {
					t.Fatalf("window accepted %s row missing %s", source, missing)
				}
			})
		}
	}
}
