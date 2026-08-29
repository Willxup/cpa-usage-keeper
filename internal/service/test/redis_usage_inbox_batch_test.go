package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type redisInboxUpdateCounter struct {
	updateCount int
}

func (c *redisInboxUpdateCounter) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return c
}

func (c *redisInboxUpdateCounter) Info(context.Context, string, ...any) {}

func (c *redisInboxUpdateCounter) Warn(context.Context, string, ...any) {}

func (c *redisInboxUpdateCounter) Error(context.Context, string, ...any) {}

func (c *redisInboxUpdateCounter) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	normalized := strings.ToLower(strings.TrimSpace(statement))
	if strings.HasPrefix(normalized, "update ") && strings.Contains(normalized, "redis_usage_inboxes") {
		c.updateCount++
	}
}

func TestProcessRedisUsageInboxBatchesProcessedMarks(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	const rowCount = 301
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, redisInboxBatchInputs("batch-mark", rowCount, now))
	if err != nil {
		t.Fatalf("seed redis usage inbox rows: %v", err)
	}

	counter := &redisInboxUpdateCounter{}
	loggedDB := db.Session(&gorm.Session{Logger: counter})
	notifier := &recordingUsageAggregationNotifier{}
	syncService := service.NewSyncServiceWithOptions(loggedDB, service.SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		Now:                      func() time.Time { return now },
		UsageAggregationNotifier: notifier,
	})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != rowCount {
		t.Fatalf("expected %d inserted events, got %+v", rowCount, result)
	}
	if counter.updateCount != 2 {
		t.Fatalf("expected 2 batched inbox UPDATE statements, got %d", counter.updateCount)
	}
	var processedCount int64
	if err := db.Model(&entities.RedisUsageInbox{}).Where("status = ?", repository.RedisUsageInboxStatusProcessed).Count(&processedCount).Error; err != nil {
		t.Fatalf("count processed inbox rows: %v", err)
	}
	if processedCount != rowCount {
		t.Fatalf("expected %d processed inbox rows, got %d", rowCount, processedCount)
	}
	var storedInbox []entities.RedisUsageInbox
	if err := db.Order("id ASC").Find(&storedInbox).Error; err != nil {
		t.Fatalf("load processed inbox rows: %v", err)
	}
	var storedEvents []entities.UsageEvent
	if err := db.Order("id ASC").Find(&storedEvents).Error; err != nil {
		t.Fatalf("load inserted usage events: %v", err)
	}
	if len(storedInbox) != rowCount || len(storedEvents) != rowCount {
		t.Fatalf("expected %d inbox rows and events, got inbox=%d events=%d", rowCount, len(storedInbox), len(storedEvents))
	}
	eventKeyByRequestID := make(map[string]string, len(storedEvents))
	for _, event := range storedEvents {
		eventKeyByRequestID[event.RequestID] = event.EventKey
	}
	for i, inbox := range storedInbox {
		expectedRequestID := fmt.Sprintf("batch-mark-%d", i)
		expectedEventKey, exists := eventKeyByRequestID[expectedRequestID]
		if !exists || expectedEventKey == "" {
			t.Fatalf("missing persisted event mapping for request_id %q", expectedRequestID)
		}
		if inbox.ID != rows[i].ID || inbox.UsageEventKey != expectedEventKey {
			t.Fatalf("unexpected inbox/event mapping at index %d: inbox=%+v expected_event_key=%q", i, inbox, expectedEventKey)
		}
	}
}

func TestProcessRedisUsageInboxRollsBackAllChunksWhenLaterProcessedMarkFails(t *testing.T) {
	db := openOpenAITokenNormalizationTestDatabase(t)
	const rowCount = 301
	now := time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, redisInboxBatchInputs("batch-rollback", rowCount, now))
	if err != nil {
		t.Fatalf("seed redis usage inbox rows: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(
		`CREATE TRIGGER fail_later_processed_mark BEFORE UPDATE OF status ON redis_usage_inboxes WHEN OLD.id = %d AND NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'later processed mark failed'); END;`,
		rows[300].ID,
	)).Error; err != nil {
		t.Fatalf("create later-batch failure trigger: %v", err)
	}

	notifier := &recordingUsageAggregationNotifier{}
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                  "https://cpa.example.com",
		Now:                      func() time.Time { return now },
		UsageAggregationNotifier: notifier,
	})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "later processed mark failed") {
		t.Fatalf("expected later processed mark failure, got result=%+v err=%v", result, err)
	}
	if notifier.usageCalls != 0 {
		t.Fatalf("expected no commit notification after rollback, got %d", notifier.usageCalls)
	}
	var eventCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count rolled-back usage events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected all usage events rolled back, got %d", eventCount)
	}
	var stored []entities.RedisUsageInbox
	if err := db.Order("id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load failed inbox rows: %v", err)
	}
	if len(stored) != rowCount {
		t.Fatalf("expected %d inbox rows, got %d", rowCount, len(stored))
	}
	for i, row := range stored {
		if row.Status != repository.RedisUsageInboxStatusProcessFailed || row.AttemptCount != 1 {
			t.Fatalf("expected process_failed retry state at index %d, got %+v", i, row)
		}
		if row.UsageEventKey != "" || row.ProcessedAt != nil {
			t.Fatalf("expected processed fields rolled back at index %d, got %+v", i, row)
		}
	}
}

func redisInboxBatchInputs(prefix string, rowCount int, poppedAt time.Time) []repositorydto.RedisInboxInsert {
	inputs := make([]repositorydto.RedisInboxInsert, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		inputs = append(inputs, repositorydto.RedisInboxInsert{
			Source: "redis_subscribe:usage",
			RawMessage: fmt.Sprintf(
				`{"timestamp":"2026-07-23T09:59:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"batch-auth","model":"gpt-5.6","request_id":"%s-%d","executor_type":"CodexExecutor","tokens":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
				prefix,
				i,
			),
			PoppedAt: poppedAt,
		})
	}
	return inputs
}
