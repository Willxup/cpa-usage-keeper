package test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func openExecutionIdentityDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "data", "issue-643-tests")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(dir, "service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(dir, "service.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func executionIdentityMessage(fields string) string {
	return `{` + fields + `,"timestamp":"2026-10-18T01:02:03+08:00","provider":"codex","api_key":"execution-key","executor_type":"CodexExecutor","model":"execution-model","trace_id":"trace-1","session_id":"session-1","node_kind":"root","is_fork":false,"is_compaction":false,"tokens":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`
}

func executionIdentityNow() time.Time { return time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC) }

func TestProcessRedisUsageExecutionOnlyEventsRemainIndependent(t *testing.T) {
	db := openExecutionIdentityDatabase(t)
	messages := []string{
		executionIdentityMessage(`"execution_id":"execution-1"`),
		executionIdentityMessage(`"execution_id":"execution-1"`),
		executionIdentityMessage(`"request_id":"","execution_id":"execution-2"`),
		executionIdentityMessage(`"request_id":"shared-request","execution_id":"execution-3"`),
		executionIdentityMessage(`"request_id":"shared-request","execution_id":"execution-4"`),
	}
	assertExecutionMessagesProcessed(t, db, messages, false)
}

func TestExecutionRecoveryRunsThroughInboxAndAggregations(t *testing.T) {
	db := openExecutionIdentityDatabase(t)
	messages := []string{
		executionIdentityMessage(`"execution_id":"recovered-execution-1"`),
		executionIdentityMessage(`"request_id":null,"execution_id":"recovered-execution-2"`),
	}
	assertExecutionMessagesProcessed(t, db, messages, true)
}

func TestExecutionRecoveryLeavesFullValidationToConsumer(t *testing.T) {
	db := openExecutionIdentityDatabase(t)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repositorydto.RedisInboxInsert{{Source: "redis_subscribe:usage", RawMessage: `{"execution_id":"needs-normal-validation","timestamp":123}`, PoppedAt: executionIdentityNow()}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", rows[0].ID).UpdateColumns(map[string]any{"status": repository.RedisUsageInboxStatusDecodeFailed, "last_error": "decode redis usage message: request_id is required"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "20261018_recover_execution_usage_events").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusPending {
		t.Fatalf("minimal ID validation should requeue: %+v error=%v", inbox, err)
	}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{Now: executionIdentityNow})
	if _, err := syncer.ProcessRedisUsageInbox(context.Background()); err == nil {
		t.Fatal("normal decoder must reject the invalid timestamp")
	}
	if err := db.First(&inbox, rows[0].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || inbox.LastError == "decode redis usage message: request_id is required" || inbox.RawMessage != rows[0].RawMessage {
		t.Fatalf("normal validation must retain raw and its own failure: %+v error=%v", inbox, err)
	}
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid complete payload must not insert an event: count=%d error=%v", count, err)
	}
}

func assertExecutionMessagesProcessed(t *testing.T, db *gorm.DB, messages []string, recoverLegacy bool) {
	t.Helper()
	inputs := make([]repositorydto.RedisInboxInsert, len(messages))
	for i, message := range messages {
		inputs[i] = repositorydto.RedisInboxInsert{Source: "redis_subscribe:usage", RawMessage: message, PoppedAt: executionIdentityNow()}
	}
	rows, err := repository.InsertRedisUsageInboxMessages(db, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if recoverLegacy {
		if err := db.Model(&entities.RedisUsageInbox{}).Where("id > 0").UpdateColumns(map[string]any{"status": repository.RedisUsageInboxStatusDecodeFailed, "last_error": "decode redis usage message: request_id is required", "attempt_count": 3}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "20261018_recover_execution_usage_events").Error; err != nil {
			t.Fatal(err)
		}
		if err := migration.Run(db); err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			var got entities.RedisUsageInbox
			if err := db.First(&got, row.ID).Error; err != nil || got.Status != repository.RedisUsageInboxStatusPending || got.AttemptCount != 0 || got.LastError != "" || got.RawMessage != row.RawMessage || !got.UpdatedAt.Equal(row.UpdatedAt) {
				t.Fatalf("recovery must requeue before normal consumer: inbox=%+v error=%v", got, err)
			}
		}
	}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{Now: executionIdentityNow})
	result, err := syncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || result.InsertedEvents != len(messages) || result.DedupedEvents != 0 {
		t.Fatalf("normal consumer: result=%+v error=%v", result, err)
	}
	var events []entities.UsageEvent
	if err := db.Order("id").Find(&events).Error; err != nil || len(events) != len(messages) {
		t.Fatalf("independent events=%d error=%v", len(events), err)
	}
	for i, event := range events {
		want, _, err := service.DecodeRedisUsageMessage(messages[i], time.Now())
		if err != nil || event.EventKey != want.EventKey || event.RequestID != want.RequestID || event.ExecutionID == nil || *event.ExecutionID != *want.ExecutionID || event.TraceID == nil || *event.TraceID != "trace-1" || event.TotalTokens != 30 || !event.Timestamp.Equal(want.Timestamp) {
			t.Fatalf("stored event changed identity/metadata/tokens: event=%+v wanted=%+v error=%v", event, want, err)
		}
		var inbox entities.RedisUsageInbox
		if err := db.First(&inbox, rows[i].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != event.EventKey || inbox.RawMessage != messages[i] || inbox.ProcessedAt == nil {
			t.Fatalf("normal transaction mapping: inbox=%+v event=%+v error=%v", inbox, event, err)
		}
	}
	var totals struct{ Requests, Tokens int64 }
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Select("SUM(request_count) AS requests, SUM(total_tokens) AS tokens").Scan(&totals).Error; err != nil || totals.Requests != int64(len(messages)) || totals.Tokens != int64(len(messages)*30) {
		t.Fatalf("normal aggregation: totals=%+v error=%v", totals, err)
	}
	result, err = syncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || result.InsertedEvents != 0 || !result.Empty {
		t.Fatalf("second processing: result=%+v error=%v", result, err)
	}
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != int64(len(messages)) {
		t.Fatalf("second processing duplicated events: count=%d error=%v", count, err)
	}
}

func TestProcessRedisExecutionIdentityRollsBackAndRetries(t *testing.T) {
	for _, failure := range []string{"insert", "processed mark"} {
		t.Run(failure, func(t *testing.T) {
			db := openExecutionIdentityDatabase(t)
			rows, err := repository.InsertRedisUsageInboxMessages(db, []repositorydto.RedisInboxInsert{{Source: "redis_subscribe:usage", RawMessage: executionIdentityMessage(`"execution_id":"rollback-execution"`), PoppedAt: executionIdentityNow()}})
			if err != nil {
				t.Fatal(err)
			}
			statement := "CREATE TRIGGER fail_execution BEFORE INSERT ON usage_events BEGIN SELECT RAISE(ABORT, 'execution rollback'); END"
			if failure == "processed mark" {
				statement = "CREATE TRIGGER fail_execution BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'execution rollback'); END"
			}
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
			notifier := &recordingUsageAggregationNotifier{}
			syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{UsageAggregationNotifier: notifier, Now: executionIdentityNow})
			if _, err := syncer.ProcessRedisUsageInbox(context.Background()); err == nil || !strings.Contains(err.Error(), "execution rollback") {
				t.Fatalf("expected rollback: %v", err)
			}
			var count int64
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil || count != 0 || notifier.usageCalls != 0 {
				t.Fatalf("partial commit: events=%d notifications=%d error=%v", count, notifier.usageCalls, err)
			}
			var inbox entities.RedisUsageInbox
			if err := db.First(&inbox, rows[0].ID).Error; err != nil || inbox.Status != repository.RedisUsageInboxStatusProcessFailed || inbox.UsageEventKey != "" || inbox.ProcessedAt != nil {
				t.Fatalf("failed transaction inbox=%+v error=%v", inbox, err)
			}
			if err := db.Exec("DROP TRIGGER fail_execution").Error; err != nil {
				t.Fatal(err)
			}
			result, err := syncer.ProcessRedisUsageInbox(context.Background())
			if err != nil || result.InsertedEvents != 1 || notifier.usageCalls != 1 {
				t.Fatalf("retry: result=%+v notifications=%d error=%v", result, notifier.usageCalls, err)
			}
			result, err = syncer.ProcessRedisUsageInbox(context.Background())
			if err != nil || result.InsertedEvents != 0 || notifier.usageCalls != 1 {
				t.Fatalf("second successful processing duplicated work: %+v error=%v", result, err)
			}
		})
	}
}

func TestExecutionOnlyRequestLogsDoNotCallCPA(t *testing.T) {
	db := openExecutionIdentityDatabase(t)
	message := executionIdentityMessage(`"execution_id":"logless-execution"`)
	event, _, err := service.DecodeRedisUsageMessage(message, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}
	client := &requestLogClientStub{}
	logs := service.NewRequestLogService(db, client)
	preview, err := logs.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogMissingID) || preview.Available {
		t.Fatalf("preview with no real request ID: %+v error=%v", preview, err)
	}
	download, err := logs.DownloadUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogMissingID) || download.Downloadable {
		t.Fatalf("download with no real request ID: %+v error=%v", download, err)
	}
	if client.fetchCalls() != 0 || client.downloadCalls != 0 {
		t.Fatalf("execution ID must not be passed as request log ID: preview=%d download=%d", client.fetchCalls(), client.downloadCalls)
	}
}
