package test

// 本文件验证 Redis usage 入站的 Token 处理、部分成功事务及下游统计链路。

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type tokenProcessorRecentRecorder struct {
	calls  int
	events []entities.UsageEvent
}

func (r *tokenProcessorRecentRecorder) TryAppend(events []entities.UsageEvent) bool {
	// 复制切片，避免生产调用方后续复用底层数组影响断言。
	r.calls++
	r.events = append(r.events, events...)
	return true
}

type tokenProcessorHeaderRecorder struct {
	calls     int
	snapshots []quota.UsageHeaderSnapshot
}

func (r *tokenProcessorHeaderRecorder) TryAppendUsageHeaderSnapshots(snapshots []quota.UsageHeaderSnapshot) bool {
	// 只记录事务提交后真正通知的 snapshot，用于证明 unresolved 行没有提前泄漏。
	r.calls++
	r.snapshots = append(r.snapshots, snapshots...)
	return true
}

func TestProcessRedisUsageInboxKnownExecutorBypassesIdentityLookup(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	// 定价只读取 Input/Output 等基础字段；错误 Total 被纠正后成本不能跟着漂移。
	multiplier := 1.0
	if err := db.Create(&entities.ModelPriceSetting{Model: "gpt-5.6", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1, CompletionPricePer1M: 2, PriceMultiplier: &multiplier}).Error; err != nil {
		t.Fatalf("seed model price: %v", err)
	}
	lookupCount := registerTokenIdentityTypeLookupCallback(t, db, nil)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source: "usage",
		RawMessage: `{
			"provider":"OpenAI",
			"auth_type":"api_key",
			"auth_index":"missing-but-not-needed",
			"model":"gpt-5.6",
			"request_id":"known-executor-no-identity",
			"executor_type":"CodexExecutor",
			"tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"total_tokens":125}
		}`,
		PoppedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}

	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("expected one completed event, got %+v", result)
	}
	if *lookupCount != 0 {
		t.Fatalf("known executor must not query usage identity type, got %d queries", *lookupCount)
	}
	event := loadTokenProcessorSyncEvent(t, db, "known-executor-no-identity")
	if event.TotalTokens != 120 {
		t.Fatalf("expected executor contract to correct nonzero Total to 120, got %+v", event)
	}

	// Overview rollup 必须消费同一条已纠正事件，不能继续保存原始错误 Total=125。
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("model = ?", "gpt-5.6").First(&hourly).Error; err != nil {
		t.Fatalf("load overview hourly stat: %v", err)
	}
	if hourly.TotalTokens != 120 {
		t.Fatalf("expected Overview rollup Total=120, got %+v", hourly)
	}

	// quota window 读取 usage_events 的 corrected Total，但成本仍按 Input=100、Output=20 计算为 0.00014。
	windowStart := event.Timestamp.Add(-time.Minute)
	windowEnd := event.Timestamp.Add(time.Minute)
	window, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "missing-but-not-needed", windowStart, &windowEnd)
	if err != nil {
		t.Fatalf("sum usage window stats: %v", err)
	}
	if window.Tokens != 120 || math.Abs(window.Cost-0.00014) > 0.000000001 {
		t.Fatalf("expected quota window tokens=120 cost=0.00014, got %+v", window)
	}

	// Request Events 服务层也必须返回同一 corrected Total 和不受 Total 改写影响的成本。
	page, err := service.NewUsageService(db).ListUsageEvents(context.Background(), servicedto.UsageFilter{StartTime: &windowStart, EndTime: &windowEnd})
	if err != nil {
		t.Fatalf("list usage events: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].TotalTokens != 120 || !page.Events[0].CostAvailable || math.Abs(page.Events[0].CostUSD-0.00014) > 0.000000001 {
		t.Fatalf("expected Request Events corrected Total and stable cost, got %+v", page.Events)
	}
}

func TestProcessRedisUsageInboxCommitsReadyItemsWhenIdentityLookupFails(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	lookupErr := errors.New("injected token identity lookup failure")
	registerTokenIdentityTypeLookupCallback(t, db, lookupErr)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
		{
			Source: "usage",
			RawMessage: `{
				"timestamp":"2026-07-14T08:00:00Z",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"ready-auth",
				"model":"gpt-5.6",
				"request_id":"ready-executor-event",
				"executor_type":"CodexExecutor",
				"tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"total_tokens":125},
				"response_headers":{"X-Codex-Primary-Used-Percent":["4"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}
			}`,
			PoppedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
		},
		{
			Source: "usage",
			RawMessage: `{
				"timestamp":"2026-07-14T08:00:01Z",
				"provider":"codex",
				"auth_type":"oauth",
				"auth_index":"unresolved-auth",
				"model":"gpt-5.6",
				"request_id":"unresolved-identity-event",
				"executor_type":"FutureExecutor",
				"tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18},
				"response_headers":{"X-Codex-Primary-Used-Percent":["5"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}
			}`,
			PoppedAt: time.Date(2026, 7, 14, 8, 0, 1, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}
	recent := &tokenProcessorRecentRecorder{}
	headers := &tokenProcessorHeaderRecorder{}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:           "https://cpa.example.com",
		RecentUsageEvents: recent,
		UsageHeaderQuota:  headers,
	})

	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), lookupErr.Error()) {
		t.Fatalf("expected partial identity lookup warning, got result=%+v err=%v", result, err)
	}
	if result == nil || result.Status != "completed_with_warnings" || result.ProcessedRows != 2 || result.InsertedEvents != 1 || !result.RetryPending {
		t.Fatalf("expected one ready success and one unresolved warning, got %+v", result)
	}
	readyEvent := loadTokenProcessorSyncEvent(t, db, "ready-executor-event")
	if readyEvent.TotalTokens != 120 {
		t.Fatalf("expected ready event to use corrected Total, got %+v", readyEvent)
	}
	assertTokenProcessorUsageEventCount(t, db, 1)

	var storedRows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&storedRows, []int64{rows[0].ID, rows[1].ID}).Error; err != nil {
		t.Fatalf("load inbox rows: %v", err)
	}
	if len(storedRows) != 2 || storedRows[0].Status != repository.RedisUsageInboxStatusProcessed || storedRows[1].Status != repository.RedisUsageInboxStatusProcessFailed || storedRows[1].AttemptCount != 1 {
		t.Fatalf("expected ready processed and unresolved retryable, got %+v", storedRows)
	}
	if recent.calls != 1 || len(recent.events) != 1 || recent.events[0].EventKey != "ready-executor-event" {
		t.Fatalf("expected only committed ready event in recent cache, got calls=%d events=%+v", recent.calls, recent.events)
	}
	if headers.calls != 1 || len(headers.snapshots) != 1 || headers.snapshots[0].AuthIndex != "ready-auth" {
		t.Fatalf("expected only committed ready snapshot, got calls=%d snapshots=%+v", headers.calls, headers.snapshots)
	}

	// 首次失败已经计一次；再处理四轮后 unresolved 行必须进入现有 discarded，成功行不能重复入库或重复通知。
	for attempt := 2; attempt <= 5; attempt++ {
		retryResult, retryErr := syncService.ProcessRedisUsageInbox(context.Background())
		if retryErr == nil || !strings.Contains(retryErr.Error(), lookupErr.Error()) {
			t.Fatalf("attempt %d expected identity lookup failure, got result=%+v err=%v", attempt, retryResult, retryErr)
		}
		// 第 2～4 次仍有待重试行；第五次确认丢弃后必须关闭等待并返回一个丢弃计数。
		if attempt < 5 {
			if retryResult == nil || !retryResult.RetryPending || retryResult.DiscardedRows != 0 {
				t.Fatalf("attempt %d expected retry pending without discard, got %+v", attempt, retryResult)
			}
		} else if retryResult == nil || retryResult.RetryPending || retryResult.DiscardedRows != 1 {
			t.Fatalf("attempt 5 expected one confirmed discard without retry pending, got %+v", retryResult)
		}
	}
	var unresolved entities.RedisUsageInbox
	if err := db.First(&unresolved, rows[1].ID).Error; err != nil {
		t.Fatalf("load unresolved inbox row: %v", err)
	}
	if unresolved.Status != repository.RedisUsageInboxStatusDiscarded || unresolved.AttemptCount != 5 {
		t.Fatalf("expected unresolved row discarded after five attempts, got %+v", unresolved)
	}
	assertTokenProcessorUsageEventCount(t, db, 1)
	if recent.calls != 1 || headers.calls != 1 {
		t.Fatalf("successful event must not repeat during unresolved retries, got recent=%d headers=%d", recent.calls, headers.calls)
	}
}

func TestProcessRedisUsageInboxWaitsWhenFailureStatusCannotBeConfirmed(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*testing.T, *gorm.DB, error)
	}{
		{
			name:   "failure status update fails",
			inject: registerTokenProcessFailureUpdateErrorCallback,
		},
		{
			name:   "updated failure status cannot be read back",
			inject: registerTokenProcessFailureReadErrorCallback,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := openOpenAITokenNormalizationTestDatabase(t)
			lookupErr := errors.New("injected uncertain token identity lookup failure")
			stateErr := errors.New("injected uncertain inbox state failure")
			registerTokenIdentityTypeLookupCallback(t, db, lookupErr)
			test.inject(t, db, stateErr)
			_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
				{
					Source:     "usage",
					RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"ready-auth","model":"gpt-5.6","request_id":"uncertain-ready","executor_type":"CodexExecutor","tokens":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
					PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
				},
				{
					Source:     "usage",
					RawMessage: `{"timestamp":"2026-07-14T08:00:01Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-uncertain","model":"future-model","request_id":"uncertain-future","executor_type":"FutureExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
					PoppedAt:   time.Date(2026, 7, 14, 8, 0, 1, 0, time.UTC),
				},
			})
			if err != nil {
				t.Fatalf("seed uncertain inbox rows: %v", err)
			}

			logs := captureTokenProcessorLogs(t)
			result, processErr := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
			if processErr == nil || !strings.Contains(processErr.Error(), lookupErr.Error()) {
				t.Fatalf("expected identity lookup warning, got result=%+v err=%v", result, processErr)
			}
			// 满批 warning 是否等待最终只读取 RetryPending；状态不明时必须按“仍可能待重试”返回 true。
			if result == nil || result.Status != "completed_with_warnings" || result.InsertedEvents != 1 || !result.RetryPending {
				t.Fatalf("expected conservative wait after uncertain failure state, got %+v", result)
			}

			entries := decodeTokenProcessorLogEntries(t, logs.String())
			unknownEntries := findAllTokenProcessorLogEntries(entries, "redis usage unknown executor could not reach identity fallback")
			if len(unknownEntries) != 0 {
				t.Fatalf("unconfirmed retry state must not add an unknown-executor warning, got %+v", unknownEntries)
			}
			// 状态更新或回读本身失败仍保留原有数据库告警，避免静默吞掉真正的状态故障。
			if !strings.Contains(logs.String(), stateErr.Error()) {
				t.Fatalf("expected the real inbox state error to remain visible: %s", logs.String())
			}
			if strings.Contains(logs.String(), "secret-uncertain") {
				t.Fatalf("failure logs must not expose auth indexes: %s", logs.String())
			}
		})
	}
}

func registerTokenIdentityTypeLookupCallback(t *testing.T, db *gorm.DB, injectedErr error) *int {
	t.Helper()
	lookupCount := 0
	callbackName := "test:tokenprocessor_identity_type_lookup"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		// 只拦截 sync 的四列 identity type 查询，不影响事务提交后的聚合查询。
		if tx.Statement == nil || tx.Statement.Table != "usage_identities" {
			return
		}
		selects := strings.Join(tx.Statement.Selects, ",")
		if !strings.Contains(selects, "auth_type, identity, type, is_deleted") {
			return
		}
		lookupCount++
		if injectedErr != nil {
			_ = tx.AddError(injectedErr)
		}
	}); err != nil {
		t.Fatalf("register identity lookup callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	return &lookupCount
}

func registerTokenProcessFailureUpdateErrorCallback(t *testing.T, db *gorm.DB, injectedErr error) {
	t.Helper()
	callbackName := "test:tokenprocessor_process_failure_update_error"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		// 只拦截包含 attempt_count 的 process-failed 更新，不能破坏 ready 行的 processed 事务。
		if !isTokenProcessFailureUpdate(tx) {
			return
		}
		_ = tx.AddError(injectedErr)
	}); err != nil {
		t.Fatalf("register process failure update callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })
}

func registerTokenProcessFailureReadErrorCallback(t *testing.T, db *gorm.DB, injectedErr error) {
	t.Helper()
	updateCallbackName := "test:tokenprocessor_process_failure_read_arm"
	queryCallbackName := "test:tokenprocessor_process_failure_read_error"
	readArmed := false
	if err := db.Callback().Update().Before("gorm:update").Register(updateCallbackName, func(tx *gorm.DB) {
		// process-failed UPDATE 即将执行时只设置一次回读故障，不影响更早的 inbox 列表查询。
		if isTokenProcessFailureUpdate(tx) {
			readArmed = true
		}
	}); err != nil {
		t.Fatalf("register process failure read arm callback: %v", err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallbackName, func(tx *gorm.DB) {
		// UPDATE 后第一次读取同一 inbox 表就是生产代码的状态确认，只在这一处注入失败。
		if !readArmed || tx.Statement == nil || tx.Statement.Table != "redis_usage_inboxes" {
			return
		}
		readArmed = false
		_ = tx.AddError(injectedErr)
	}); err != nil {
		t.Fatalf("register process failure read callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(updateCallbackName)
		_ = db.Callback().Query().Remove(queryCallbackName)
	})
}

func isTokenProcessFailureUpdate(tx *gorm.DB) bool {
	if tx.Statement == nil || tx.Statement.Table != "redis_usage_inboxes" {
		return false
	}
	updates, ok := tx.Statement.Dest.(map[string]any)
	if !ok {
		return false
	}
	_, changesAttemptCount := updates["attempt_count"]
	return changesAttemptCount
}

func loadTokenProcessorSyncEvent(t *testing.T, db *gorm.DB, eventKey string) entities.UsageEvent {
	t.Helper()
	var event entities.UsageEvent
	if err := db.Where("event_key = ?", eventKey).First(&event).Error; err != nil {
		t.Fatalf("load usage event %q: %v", eventKey, err)
	}
	return event
}

func assertTokenProcessorUsageEventCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d usage events, got %d", expected, count)
	}
}
