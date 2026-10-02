package test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

// TestSyncPricingUsesNormalizedEventAndCPATime 验证入库金额使用最终 Token、九个请求维度及 CPA 事件时间。
func TestSyncPricingUsesNormalizedEventAndCPATime(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	config := completeServiceConfig("billable-alias", 1)
	for key, value := range map[string]string{
		"api_group_key": "group-a", "model": "unlisted-model", "auth_index": "auth-a",
		"model_alias": "billable-alias", "service_tier": "priority", "response_service_tier": "fast",
		"reasoning_effort": "high", "endpoint": "/v1/responses", "executor_type": "CodexExecutor",
	} {
		config.ConditionalMultipliers = append(config.ConditionalMultipliers, pricing.RuleConfig{Key: key, Value: value, Multiplier: 2})
	}
	threshold, start, end := int64(99), "20:00", "08:00"
	config.Branches = []pricing.PriceBranch{{
		ID: "night-large", Name: "Night large context",
		Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold},
		Period:  pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: &start, End: &end},
		Prices:  pricing.BasePrices{Input: 4, Output: 6, CacheRead: 0.5, CacheWrite: 1},
	}}
	if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
		t.Fatalf("save full price: %v", err)
	}
	// 两个 TZ 下都构造部署当地 21:00，证明分支由已存 CPA 时间而非处理时钟选择。
	when := time.Date(2026, 9, 23, 21, 0, 0, 0, time.Local)
	message := fmt.Sprintf(`{"timestamp":%q,"api_key":"group-a","model":"unlisted-model","alias":"billable-alias","auth_index":"auth-a","service_tier":"priority","response_service_tier":"fast","reasoning_effort":"high","endpoint":"/v1/responses","executor_type":"CodexExecutor","request_id":"priced-alias","tokens":{"input_tokens":100,"output_tokens":20,"cached_tokens":30,"cache_creation_tokens":10,"total_tokens":120}}`, when.Format(time.RFC3339))
	seedRedisInboxMessagesForTest(t, db, message)
	cache := &recordingRecentUsageAppender{allowed: true}
	notifier := &recordingUsageAggregationNotifier{}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		PricingCatalog: catalog, RecentUsageEvents: cache, UsageAggregationNotifier: notifier,
		Now: func() time.Time { return when.Add(12 * time.Hour) },
	})
	result, err := syncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("process priced event: result=%+v err=%v", result, err)
	}
	event := loadTokenProcessorSyncEvent(t, db, "priced-alias")
	if event.InputTokens != 100 || event.CacheReadTokens != 30 || event.CacheCreationTokens != 10 || event.OutputTokens != 20 || !event.Timestamp.Equal(when) {
		t.Fatalf("normalized price input changed: %+v", event)
	}
	// 60 uncached input、30 cache read、10 cache write、20 output 使用夜间整组单价及九个 2 倍规则。
	want := (60*4 + 30*0.5 + 10*1 + 20*6) * 512 / 1_000_000.0
	assertStoredSyncFee(t, event, want, true)
	if len(cache.events) != 1 || len(notifier.events) != 1 {
		t.Fatalf("committed event notifications missing: cache=%d aggregation=%d", len(cache.events), len(notifier.events))
	}
	assertStoredSyncFee(t, cache.events[0], want, true)
	assertStoredSyncFee(t, notifier.events[0], want, true)
}

// TestSyncPricingChoosesContextBranchAfterClaudeNormalization 验证输入缓存合并先于上下文分支选择。
func TestSyncPricingChoosesContextBranchAfterClaudeNormalization(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	config := completeServiceConfig("claude-model", 1)
	threshold := int64(99)
	config.Branches = []pricing.PriceBranch{{
		ID: "large-context", Name: "Large context",
		Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold},
		Period:  pricing.PeriodCondition{Type: pricing.PeriodAll},
		Prices:  pricing.BasePrices{Input: 4, Output: 6, CacheRead: 0.5, CacheWrite: 1},
	}}
	if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-09-23T08:00:00Z","model":"claude-model","executor_type":"ClaudeExecutor","request_id":"claude-context","tokens":{"input_tokens":80,"output_tokens":20,"cache_read_tokens":30,"cache_creation_tokens":10,"total_tokens":140}}`)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	if result, err := syncer.ProcessRedisUsageInbox(context.Background()); err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("process Claude event: result=%+v err=%v", result, err)
	}
	event := loadTokenProcessorSyncEvent(t, db, "claude-context")
	if event.InputTokens != 120 || event.CacheReadTokens != 30 || event.CacheCreationTokens != 10 || event.OutputTokens != 20 {
		t.Fatalf("Claude event not normalized before pricing: %+v", event)
	}
	// Context 用 120 命中分支；费用中的普通输入仍只计原始 80，不重复计两个缓存段。
	want := (80*4 + 30*0.5 + 10*1 + 20*6) / 1_000_000.0
	assertStoredSyncFee(t, event, want, true)
}

// TestSyncPricingMissingFreeAndDuplicateMessages 验证缺价与合法免费不同，且同内容消息各保留事件。
func TestSyncPricingMissingFreeAndDuplicateMessages(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("free-model", 0)); err != nil {
		t.Fatal(err)
	}
	missing := `{"timestamp":"2026-09-23T08:00:00Z","model":"missing-model","request_id":"same-request","tokens":{"input_tokens":1000000}}`
	free := `{"timestamp":"2026-09-23T08:00:00Z","model":"free-model","request_id":"free-request","tokens":{"input_tokens":1000000}}`
	rows := seedRedisInboxMessagesForTest(t, db, missing, missing, free)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	result, err := syncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || result == nil || result.InsertedEvents != 3 {
		t.Fatalf("process duplicate and free messages: result=%+v err=%v", result, err)
	}
	var events []entities.UsageEvent
	if err := db.Order("id").Find(&events).Error; err != nil || len(events) != 3 {
		t.Fatalf("load distinct events: count=%d err=%v", len(events), err)
	}
	for index := range events {
		assertStoredSyncFee(t, events[index], 0, index == 2)
		if index < 2 && events[index].EventKey != "same-request" {
			t.Fatalf("duplicate message was merged or reordered: %+v", events)
		}
		var row entities.RedisUsageInbox
		if err := db.First(&row, rows[index].ID).Error; err != nil || row.Status != repository.RedisUsageInboxStatusProcessed {
			t.Fatalf("inbox %d not processed with event: %+v err=%v", index, row, err)
		}
	}
}

// TestSyncPricingRollbackAndRetry 验证 processed 标记失败会连同已计价事件回滚，重试只产生一条。
func TestSyncPricingRollbackAndRetry(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("priced-model", 2)); err != nil {
		t.Fatal(err)
	}
	rows := seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-09-23T08:00:00Z","model":"priced-model","request_id":"retry-price","tokens":{"input_tokens":1000000}}`)
	if err := db.Exec(`CREATE TRIGGER fail_priced_processed BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'processed mark failed'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	if result, err := syncer.ProcessRedisUsageInbox(context.Background()); err == nil || result == nil || result.Status != "failed" {
		t.Fatalf("expected atomic failure: result=%+v err=%v", result, err)
	}
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed processed mark kept priced event: count=%d err=%v", count, err)
	}
	var row entities.RedisUsageInbox
	if err := db.First(&row, rows[0].ID).Error; err != nil || row.Status != repository.RedisUsageInboxStatusProcessFailed {
		t.Fatalf("failed row not retryable: %+v err=%v", row, err)
	}
	if err := db.Exec(`DROP TRIGGER fail_priced_processed`).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := syncer.ProcessRedisUsageInbox(context.Background()); err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("retry priced event: result=%+v err=%v", result, err)
	}
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retry did not produce exactly one event: count=%d err=%v", count, err)
	}
	assertStoredSyncFee(t, loadTokenProcessorSyncEvent(t, db, "retry-price"), 2, true)
}

type blockingPricedRecentAppender struct {
	entered chan struct{}
	release chan struct{}
}

func (a *blockingPricedRecentAppender) TryAppend([]entities.UsageEvent) bool {
	close(a.entered)
	<-a.release
	return true
}

// TestSyncPricingSaveDoesNotWaitForCommittedBatch 验证提交后的通知仍未返回时普通改价可以完成。
func TestSyncPricingSaveDoesNotWaitForCommittedBatch(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 1)); err != nil {
		t.Fatal(err)
	}
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-09-23T08:00:00Z","model":"model-a","request_id":"old-price","tokens":{"input_tokens":1000000}}`)
	appender := &blockingPricedRecentAppender{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(appender.release) }) }
	defer release()
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, RecentUsageEvents: appender, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	processed := make(chan error, 1)
	go func() {
		_, err := syncer.ProcessRedisUsageInbox(context.Background())
		processed <- err
	}()
	select {
	case <-appender.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("priced batch did not reach post-commit notification")
	}
	saved := make(chan error, 1)
	go func() {
		_, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 2))
		saved <- err
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatalf("save while batch not returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary price save waited for post-commit batch notification")
	}
	release()
	if err := <-processed; err != nil {
		t.Fatalf("old-price batch: %v", err)
	}
	assertStoredSyncFee(t, loadTokenProcessorSyncEvent(t, db, "old-price"), 1, true)
	seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-09-23T08:01:00Z","model":"model-a","request_id":"new-price","tokens":{"input_tokens":1000000}}`)
	nextSyncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, UsageAggregationNotifier: &recordingUsageAggregationNotifier{}})
	if result, err := nextSyncer.ProcessRedisUsageInbox(context.Background()); err != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("new-price batch: result=%+v err=%v", result, err)
	}
	assertStoredSyncFee(t, loadTokenProcessorSyncEvent(t, db, "new-price"), 2, true)
}

// TestSyncPricingRejectsMissingCatalogBeforeConsumingInbox 防止未注入价格依赖时真实事件被写成缺价。
func TestSyncPricingRejectsMissingCatalogBeforeConsumingInbox(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	rows := seedRedisInboxMessagesForTest(t, db, `{"timestamp":"2026-09-23T08:00:00Z","model":"model-a","request_id":"missing-catalog","tokens":{"input_tokens":1}}`)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{})
	if _, err := syncer.ProcessRedisUsageInbox(context.Background()); err == nil || !strings.Contains(err.Error(), "pricing catalog") {
		t.Fatalf("missing catalog was accepted: %v", err)
	}
	var row entities.RedisUsageInbox
	if err := db.First(&row, rows[0].ID).Error; err != nil || row.Status != repository.RedisUsageInboxStatusPending || row.AttemptCount != 0 {
		t.Fatalf("missing dependency consumed inbox retry: %+v err=%v", row, err)
	}
}

func assertStoredSyncFee(t *testing.T, event entities.UsageEvent, want float64, available bool) {
	t.Helper()
	if event.CostUSD == nil || event.CostAvailable == nil {
		t.Fatalf("event fee is NULL, want (%v,%v): %+v", want, available, event)
	}
	if *event.CostAvailable != available || math.Abs(*event.CostUSD-want) > 1e-9 {
		t.Fatalf("event fee = (%v,%v), want (%v,%v): %+v", *event.CostUSD, *event.CostAvailable, want, available, event)
	}
}
