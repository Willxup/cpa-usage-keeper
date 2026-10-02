package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageRecentEventCacheKeepsStoredCostAcrossLoadAppendAndReads(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	// 当前模型报价故意与事件已存金额不符；缓存不得据价格重新估算历史事件。
	if err := db.Create(&entities.ModelPriceSetting{
		Model: "priced-model", PricingStyle: entities.ModelPricingStyleOpenAI,
		PromptPricePer1M: 999, CompletionPricePer1M: 999,
	}).Error; err != nil {
		t.Fatal(err)
	}
	storedCost, available := 1.25, true
	zeroCost, unavailable := 0.0, false
	seeds := []entities.UsageEvent{
		{EventKey: "stored-cost", Model: "priced-model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-20 * time.Minute), InputTokens: 1000, TotalTokens: 1000, CostUSD: &storedCost, CostAvailable: &available},
		{EventKey: "missing-price", Model: "unpriced-model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-19 * time.Minute), TotalTokens: 1, CostUSD: &zeroCost, CostAvailable: &unavailable},
		{EventKey: "unbackfilled", Model: "old-model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-18 * time.Minute), TotalTokens: 1},
	}
	if err := db.Create(&seeds).Error; err != nil {
		t.Fatal(err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	beforeHealth, ok := cache.CredentialHealth("oauth", "auth-a", now)
	if !ok || beforeHealth.TotalSuccess != 3 {
		t.Fatalf("旧健康桶未建立: %+v ok=%t", beforeHealth, ok)
	}
	cached, ok := cache.Events(now.Add(-time.Hour), now, false, "")
	if !ok || len(cached) != 3 {
		t.Fatalf("启动加载的费用事件缺失: %+v ok=%t", cached, ok)
	}
	var fromDB []entities.UsageEvent
	if err := db.Order("timestamp asc").Find(&fromDB).Error; err != nil {
		t.Fatal(err)
	}
	if len(fromDB) != 3 {
		t.Fatalf("数据库来源事件数错误: %d", len(fromDB))
	}
	for index := range cached {
		assertRecentCostMatchesStoredEvent(t, cached[index], fromDB[index])
	}
	if cached[0].CostUSD == nil || *cached[0].CostUSD != 1.25 || cached[0].CostAvailable == nil || !*cached[0].CostAvailable {
		t.Fatalf("缓存按当前报价改变了已存费用: %+v", cached[0])
	}
	if cached[1].CostUSD == nil || *cached[1].CostUSD != 0 || cached[1].CostAvailable == nil || *cached[1].CostAvailable {
		t.Fatalf("显式零费用/缺价状态丢失: %+v", cached[1])
	}
	if cached[2].CostUSD != nil || cached[2].CostAvailable != nil {
		t.Fatalf("未回填费用被伪装成明确零值: %+v", cached[2])
	}
	// 返回值和原始追加 batch 都可被调用方复用；缓存持有独立费用指针。
	*cached[0].CostUSD = 77
	*cached[0].CostAvailable = false
	*cached[1].CostAvailable = true
	appendCost, appendAvailable := 2.5, true
	appended := []entities.UsageEvent{{EventKey: "after-commit", Model: "priced-model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-time.Minute), CostUSD: &appendCost, CostAvailable: &appendAvailable}}
	if err := db.Create(&appended[0]).Error; err != nil {
		t.Fatal(err)
	}
	if !cache.TryAppend(appended) {
		t.Fatal("已提交事件未进入缓存队列")
	}
	appendCost, appendAvailable = 99, false
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, ok := cache.EventsSince(now.Add(-time.Hour), "")
	if !ok || len(again) != 4 {
		t.Fatalf("追加后范围返回不完整: %+v ok=%t", again, ok)
	}
	if again[0].CostUSD == nil || *again[0].CostUSD != 1.25 || again[0].CostAvailable == nil || !*again[0].CostAvailable ||
		again[1].CostAvailable == nil || *again[1].CostAvailable ||
		again[3].CostUSD == nil || *again[3].CostUSD != 2.5 || again[3].CostAvailable == nil || !*again[3].CostAvailable {
		t.Fatalf("读取或追加时费用指针污染缓存: %+v", again)
	}
	var appendedFromDB entities.UsageEvent
	if err := db.Where("event_key = ?", "after-commit").Take(&appendedFromDB).Error; err != nil {
		t.Fatal(err)
	}
	assertRecentCostMatchesStoredEvent(t, again[3], appendedFromDB)
	*again[3].CostUSD = 42
	*again[3].CostAvailable = false
	third, _ := cache.Events(now.Add(-time.Hour), now, false, "")
	if len(third) != 4 || third[3].CostUSD == nil || *third[3].CostUSD != 2.5 || third[3].CostAvailable == nil || !*third[3].CostAvailable {
		t.Fatalf("EventsSince 返回值污染后续 Events: %+v", third)
	}
	afterHealth, ok := cache.CredentialHealth("oauth", "auth-a", now)
	if !ok || afterHealth.TotalSuccess != 4 || len(afterHealth.Buckets) != len(beforeHealth.Buckets) {
		t.Fatalf("费用追加破坏了原健康桶生命周期: before=%+v after=%+v", beforeHealth, afterHealth)
	}
}

func assertRecentCostMatchesStoredEvent(t *testing.T, recent repository.RecentUsageEvent, stored entities.UsageEvent) {
	t.Helper()
	if (recent.CostUSD == nil) != (stored.CostUSD == nil) || (recent.CostAvailable == nil) != (stored.CostAvailable == nil) {
		t.Fatalf("缓存与 DB 的 NULL 费用状态不一致: recent=%+v stored=%+v", recent, stored)
	}
	if recent.CostUSD != nil && *recent.CostUSD != *stored.CostUSD {
		t.Fatalf("缓存金额来自当前报价而非 DB: recent=%+v stored=%+v", recent, stored)
	}
	if recent.CostAvailable != nil && *recent.CostAvailable != *stored.CostAvailable {
		t.Fatalf("缓存可用性与 DB 不一致: recent=%+v stored=%+v", recent, stored)
	}
}
