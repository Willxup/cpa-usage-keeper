package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceRequestListAndStreamKeepStoredCostAfterPricingChanges(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model: "model-a", PromptPricePer1M: 100, CompletionPricePer1M: 200,
	}); err != nil {
		t.Fatal(err)
	}
	original, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(original)
	provider := service.NewUsageService(db, catalog)
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	when := start.Add(time.Minute)
	storedCost, available := 1.25, true
	zeroCost, unavailable := 0.0, false
	events := []entities.UsageEvent{
		{EventKey: "stored", Model: "model-a", Timestamp: when, InputTokens: 1000, TotalTokens: 1000, CostUSD: &storedCost, CostAvailable: &available},
		{EventKey: "free-unavailable", Model: "model-a", Timestamp: when.Add(time.Minute), TotalTokens: 50, CostUSD: &zeroCost, CostAvailable: &unavailable},
		{EventKey: "other-model", Model: "model-b", Timestamp: when.Add(2 * time.Minute), TotalTokens: 99, CostUSD: &zeroCost, CostAvailable: &unavailable},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatal(err)
	}
	filter := servicedto.UsageFilter{Range: "custom", StartTime: &start, EndTime: &end, Model: "model-a", Page: 1, PageSize: 10, Limit: 10}
	assertRequestCosts := func(want float64, wantStyle string) {
		t.Helper()
		page, err := provider.ListUsageEvents(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalCount != 2 || len(page.Events) != 2 {
			t.Fatalf("列表筛选或分页被费用切换改变: %+v", page)
		}
		streamed := []servicedto.UsageEventRecord{}
		if err := provider.StreamUsageEvents(context.Background(), filter, func(row servicedto.UsageEventRecord) error {
			streamed = append(streamed, row)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(streamed) != 2 {
			t.Fatalf("导出流与列表筛选不一致: %+v", streamed)
		}
		for _, rows := range [][]servicedto.UsageEventRecord{page.Events, streamed} {
			byID := map[int64]servicedto.UsageEventRecord{}
			for _, row := range rows {
				byID[row.ID] = row
			}
			if byID[events[0].ID].CostUSD != want || !byID[events[0].ID].CostAvailable || byID[events[0].ID].PricingStyle != wantStyle ||
				byID[events[1].ID].CostUSD != 0 || byID[events[1].ID].CostAvailable {
				t.Fatalf("列表/导出未使用已存费用: %+v", rows)
			}
		}
	}
	assertRequestCosts(1.25, "openai")
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model: "model-a", PromptPricePer1M: 999, CompletionPricePer1M: 999,
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(changed)
	assertRequestCosts(1.25, "openai")
	if err := db.Where("model = ?", "model-a").Delete(&entities.ModelPriceSetting{}).Error; err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(deleted)
	assertRequestCosts(1.25, "")
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", events[0].ID).Update("cost_usd", 4.5).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestCosts(4.5, "")
}

func TestUsageServiceRequestListAndStreamRejectUnbackfilledCost(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	when := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "unbackfilled", Model: "model-a", Timestamp: when, TotalTokens: 1}}); err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageService(db, pricing.NewCatalog(pricing.EmptySnapshot()))
	start, end := when.Add(-time.Minute), when.Add(time.Minute)
	filter := servicedto.UsageFilter{Range: "custom", StartTime: &start, EndTime: &end, Model: "model-a", Page: 1, PageSize: 10, Limit: 10}
	if _, err := provider.ListUsageEvents(context.Background(), filter); err == nil {
		t.Fatal("NULL 费用被列表伪装成明确零金额")
	}
	if err := provider.StreamUsageEvents(context.Background(), filter, func(servicedto.UsageEventRecord) error { return nil }); err == nil {
		t.Fatal("NULL 费用被导出流伪装成明确零金额")
	}
}
