package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestProcessRedisUsageInboxPreservesImageTotalOnlyUsage(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	multiplier := 1.0
	if err := db.Create(&entities.ModelPriceSetting{Model: "gpt-image-2", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1, CompletionPricePer1M: 2, PriceMultiplier: &multiplier}).Error; err != nil {
		t.Fatal(err)
	}
	// 使用 issue #636 的完整 Token 形态，验证入库而非仅测试纯处理器。
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
   "timestamp":"2026-10-13T08:00:00Z",
   "provider":"codex", "executor_type":"CodexExecutor", "auth_type":"apikey",
   "model":"gpt-image-2", "alias":"gpt-image-2", "endpoint":"POST /v1/images/edits",
   "execution_id":"fixture-execution","request_id":"image-total-only", "stream":false, "failed":false,
   "tokens":{"input_tokens":0,"output_tokens":0,"reasoning_tokens":0,"cached_tokens":0,"cache_read_tokens":0,"cache_read_tokens_present":true,"cache_creation_tokens":0,"total_tokens":2120}
  }`,
		PoppedAt: time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	event := loadTokenProcessorSyncEvent(t, db, "image-total-only")
	if event.TotalTokens != 2120 || event.InputTokens != 0 || event.OutputTokens != 0 || event.ReasoningTokens != 0 || event.CachedTokens != 0 || event.CacheReadTokens != 0 || event.CacheCreationTokens != 0 {
		t.Fatalf("expected total-only event without fabricated breakdown: %+v", event)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, event.Timestamp.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("model = ?", "gpt-image-2").First(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	if hourly.TotalTokens != 2120 || hourly.InputTokens != 0 || hourly.OutputTokens != 0 {
		t.Fatalf("unexpected overview: %+v", hourly)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	start, end := event.Timestamp.Add(-time.Minute), event.Timestamp.Add(time.Minute)
	page, err := service.NewUsageServiceWithOptions(db, service.UsageServiceOptions{PricingCatalog: pricing.NewCatalog(snapshot)}).ListUsageEvents(context.Background(), servicedto.UsageFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatal(err)
	}
	// 即使配置了非零单价，也不能把未分类 Total 当成输入或输出来收费。
	if len(page.Events) != 1 || page.Events[0].TotalTokens != 2120 || math.Abs(page.Events[0].CostUSD) > 1e-9 {
		t.Fatalf("unexpected request event totals/cost: %+v", page.Events)
	}
}
