package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
)

// 同模型不同条件事件已分别持久化费用，窗口只求和且不再读规则维度。
func TestUsageWindowRawSumsStoredFeesAcrossPricingDimensions(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "raw-priority", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "priority", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(2), CostAvailable: windowAvailablePtr(true)},
		{EventKey: "raw-default", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "default", Timestamp: start.Add(20 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(1), CostAvailable: windowAvailablePtr(true)},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed raw window events: %v", err)
	}
	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil {
		t.Fatalf("sum raw window: %v", err)
	}
	if stats.Tokens != 2_000_000 || stats.Cost != 3 || !stats.CostAvailable {
		t.Fatalf("expected stored raw fees 1+2, got %+v", stats)
	}
}

// 长窗口左边界与完整小时的原条件可不同，但已存总费用可直接安全相加。
func TestUsageWindowLongSumsStoredRawAndHourlyFeesAcrossDimensions(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 20, 12, 30, 0, 0, time.Local)
	end := start.Add(7*24*time.Hour + 20*time.Minute)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "left-priority", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "priority", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000,
		CostUSD: windowCostPtr(2), CostAvailable: windowAvailablePtr(true),
	}).Error; err != nil {
		t.Fatalf("seed left raw event: %v", err)
	}
	hour := start.Add(2 * time.Hour).Truncate(time.Hour)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: hour, AuthIndex: "auth-a", Model: "model-a", ServiceTier: "default", InputTokens: 1_000_000, TotalTokens: 1_000_000,
		CostUSD: windowCostPtr(1), UnavailableCostCount: windowUnavailablePtr(0),
	}).Error; err != nil {
		t.Fatalf("seed hourly window row: %v", err)
	}
	stats, err := newUsageWindowCalculatorForTest(t, db).SumByAuthIndex(context.Background(), "auth-a", start, &end)
	if err != nil {
		t.Fatalf("sum long window: %v", err)
	}
	if stats.Tokens != 2_000_000 || stats.Cost != 3 || !stats.CostAvailable {
		t.Fatalf("expected stored raw/hourly fees 2+1, got %+v", stats)
	}
}
