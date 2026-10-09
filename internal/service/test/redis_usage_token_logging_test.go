package test

// 本文件验证 Redis usage 入站的 Token outcome、action 与异常日志边界。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestMixedBatchDoesNotWarnBeforeUnknownExecutorIsDiscarded(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	lookupErr := errors.New("injected mixed token identity lookup failure")
	registerTokenIdentityTypeLookupCallback(t, db, lookupErr)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"secret-ready","model":"gpt-5.6","request_id":"mixed-ready","executor_type":"CodexExecutor","tokens":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:01Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-future","model":"future-model","request_id":"mixed-future","executor_type":"FutureExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 1, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:02Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-placeholder","model":"legacy-model","request_id":"mixed-placeholder","executor_type":"unknown","tokens":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 2, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("seed mixed inbox rows: %v", err)
	}

	logs := captureTokenProcessorLogs(t)
	result, processErr := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if processErr == nil || !strings.Contains(processErr.Error(), lookupErr.Error()) || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one ready event plus identity warning, got result=%+v err=%v", result, processErr)
	}

	entries := decodeTokenProcessorLogEntries(t, logs.String())
	unknownEntries := findAllTokenProcessorLogEntries(entries, "redis usage unknown executor could not reach identity fallback")
	if len(unknownEntries) != 0 {
		t.Fatalf("retryable identity failure must not emit an unknown-executor warning, got %+v", unknownEntries)
	}
	if strings.Contains(logs.String(), "secret-future") || strings.Contains(logs.String(), "secret-placeholder") {
		t.Fatalf("retry logs must not expose credential identity: %s", logs.String())
	}
}

func TestUnknownExecutorOnlyWarnsAfterConfirmedDiscard(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	lookupErr := errors.New("injected repeated token identity lookup failure")
	registerTokenIdentityTypeLookupCallback(t, db, lookupErr)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source:     "usage",
		RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-retry","model":"future-model","request_id":"discarded-future","executor_type":"FutureExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
		PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed retry inbox row: %v", err)
	}

	logs := captureTokenProcessorLogs(t)
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})
	for attempt := 1; attempt <= 5; attempt++ {
		if _, processErr := syncService.ProcessRedisUsageInbox(context.Background()); processErr == nil || !strings.Contains(processErr.Error(), lookupErr.Error()) {
			t.Fatalf("attempt %d expected identity lookup failure, got %v", attempt, processErr)
		}
	}

	entries := decodeTokenProcessorLogEntries(t, logs.String())
	unknownEntries := findAllTokenProcessorLogEntries(entries, "redis usage unknown executor could not reach identity fallback")
	if len(unknownEntries) != 0 {
		t.Fatalf("retry lifecycle must not emit a second unknown-executor warning, got %+v", unknownEntries)
	}
	discardedEntries := findAllTokenProcessorLogEntries(entries, "discarded redis usage inbox row after repeated process failures")
	if len(discardedEntries) != 1 || discardedEntries[0].AttemptCount != 5 {
		t.Fatalf("expected exactly one warning after the fifth confirmed discard, got %+v", discardedEntries)
	}
}

func TestTokenAttentionLogsOnlyAfterCommittedTransaction(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source:     "usage",
		RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"commit-gate","model":"gpt-5.6","request_id":"uncommitted-correction","executor_type":"CodexExecutor","tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"total_tokens":125}}`,
		PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed uncommitted correction inbox row: %v", err)
	}

	persistErr := errors.New("injected usage event persistence failure")
	registerTokenUsageInsertErrorCallback(t, db, persistErr)
	logs := captureTokenProcessorLogs(t)
	result, processErr := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if processErr == nil || !strings.Contains(processErr.Error(), persistErr.Error()) || result == nil || !result.RetryPending {
		t.Fatalf("expected retryable persistence failure, got result=%+v err=%v", result, processErr)
	}

	entries := decodeTokenProcessorLogEntries(t, logs.String())
	if attention := findAllTokenProcessorLogEntries(entries, "redis usage token event requires attention"); len(attention) != 0 {
		t.Fatalf("uncommitted event must not emit token attention logs, got %+v", attention)
	}
}

func TestSuccessfulMissingIdentityFallbackKeepsOneWarningPerEvent(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"Unknown","auth_type":"api_key","auth_index":"missing-empty","model":"empty-model","request_id":"missing-empty-executor","executor_type":"","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:01Z","provider":"Unknown","auth_type":"api_key","auth_index":"missing-placeholder","model":"placeholder-model","request_id":"missing-placeholder-executor","executor_type":"unknown","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 1, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:02Z","provider":"Unknown","auth_type":"api_key","auth_index":"missing-future","model":"future-model","request_id":"missing-future-executor","executor_type":"FutureExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 2, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("seed missing identity executor matrix: %v", err)
	}

	logs := captureTokenProcessorLogs(t)
	result, processErr := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if processErr != nil || result == nil || result.InsertedEvents != 3 {
		t.Fatalf("expected three successful strict fallback events, got result=%+v err=%v", result, processErr)
	}

	entries := decodeTokenProcessorLogEntries(t, logs.String())
	missingEntries := findAllTokenProcessorLogEntries(entries, "usage identity type not found for redis usage event")
	if len(missingEntries) != 2 || missingEntries[0].EventKey != "missing-empty-executor" || missingEntries[1].EventKey != "missing-placeholder-executor" {
		t.Fatalf("expected legacy missing-identity warnings only for empty and CPA placeholder executors, got %+v", missingEntries)
	}
	attentionEntries := findAllTokenProcessorLogEntries(entries, "redis usage token event requires attention")
	if len(attentionEntries) != 1 || attentionEntries[0].EventKey != "missing-future-executor" {
		t.Fatalf("expected one routing observation for the real future executor, got %+v", attentionEntries)
	}
}

func TestProcessRedisUsageInboxLogsTokenSummaryAndOnlyExceptionalEvents(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"secret-corrected","model":"gpt-5.6","request_id":"log-corrected","executor_type":"CodexExecutor","tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"total_tokens":125}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:01Z","provider":"Claude","auth_type":"oauth","auth_index":"secret-normalized","model":"claude-sonnet","request_id":"log-normalized","executor_type":"ClaudeExecutor","tokens":{"input_tokens":100,"output_tokens":20,"cache_read_tokens":10,"total_tokens":130}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 1, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:02Z","provider":"Compatibility","auth_type":"api_key","auth_index":"secret-compatibility","model":"gemini-2.5-pro","request_id":"log-compatibility","executor_type":"OpenAICompatExecutor","tokens":{"input_tokens":1000,"output_tokens":20,"reasoning_tokens":50,"total_tokens":1070}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 2, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:03Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-unknown","raw_secret_marker":"sk-raw-payload-only-marker","model":"future-model","request_id":"log-unknown","executor_type":"FutureExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 3, 0, time.UTC),
		},
		{
			Source:     "usage",
			RawMessage: `{"timestamp":"2026-07-14T08:00:04Z","provider":"Unknown","auth_type":"api_key","auth_index":"secret-cpa-unknown","model":"legacy-model","request_id":"log-cpa-unknown","executor_type":"unknown","tokens":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}`,
			PoppedAt:   time.Date(2026, 7, 14, 8, 0, 4, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("seed inbox rows: %v", err)
	}

	logs := captureTokenProcessorLogs(t)
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 5 {
		t.Fatalf("expected five completed events, got %+v", result)
	}

	entries := decodeTokenProcessorLogEntries(t, logs.String())
	summaries := findAllTokenProcessorLogEntries(entries, "redis usage token processing summary")
	if len(summaries) != 1 {
		t.Fatalf("expected batch token summary log, got %s", logs.String())
	}
	summary := summaries[0]
	if summary.UnknownExecutorCount != 1 || summary.TokenReadyCount != 5 {
		t.Fatalf("unexpected summary routing counts: %+v", summary)
	}
	outcomes := summary.OutcomeCounts
	if outcomes["corrected"] != 1 || outcomes["normalized"] != 1 || outcomes["compatibility"] != 1 || outcomes["valid"] != 2 {
		t.Fatalf("unexpected token outcome counts: %+v", summary.OutcomeCounts)
	}
	actions := summary.ActionCounts
	if actions["correct_nonzero_total"] != 1 || actions["apply_issue272_reasoning_fold"] != 1 {
		t.Fatalf("unexpected token action counts: %+v", summary.ActionCounts)
	}

	attentionEvents := tokenProcessorAttentionEventKeys(entries)
	if len(attentionEvents) != 2 || !attentionEvents["log-corrected"] || !attentionEvents["log-unknown"] {
		t.Fatalf("expected event logs only for corrected and unknown, got %+v", attentionEvents)
	}
	if strings.Contains(logs.String(), "secret-corrected") || strings.Contains(logs.String(), "secret-normalized") || strings.Contains(logs.String(), "secret-compatibility") || strings.Contains(logs.String(), "secret-unknown") || strings.Contains(logs.String(), "secret-cpa-unknown") || strings.Contains(logs.String(), "sk-raw-payload-only-marker") {
		t.Fatalf("token logs must not expose auth indexes or credentials: %s", logs.String())
	}
}

func TestTokenBatchSummaryRemainsDebugOnly(t *testing.T) {
	// 生产默认 info 级别不增加批次汇总日志；需要排查时仍可显式启用 debug。
	db := openUsageServiceTestDatabase(t)
	_, err := repository.InsertRedisUsageInboxMessages(db, []repodto.RedisInboxInsert{{
		Source:     "usage",
		RawMessage: `{"timestamp":"2026-07-14T08:00:00Z","provider":"OpenAI","auth_type":"api_key","auth_index":"summary-info","model":"gpt-5.6","request_id":"summary-info-event","executor_type":"CodexExecutor","tokens":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`,
		PoppedAt:   time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed info-level summary inbox row: %v", err)
	}

	logs := captureTokenProcessorLogsAtLevel(t, logrus.InfoLevel)
	result, processErr := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com"}).ProcessRedisUsageInbox(context.Background())
	if processErr != nil || result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected one normal event, got result=%+v err=%v", result, processErr)
	}
	if strings.Contains(logs.String(), "redis usage token processing summary") {
		t.Fatalf("info-level production logs must not include debug batch summary: %s", logs.String())
	}
}

func captureTokenProcessorLogs(t *testing.T) *bytes.Buffer {
	return captureTokenProcessorLogsAtLevel(t, logrus.DebugLevel)
}

func registerTokenUsageInsertErrorCallback(t *testing.T, db *gorm.DB, injectedErr error) {
	t.Helper()
	callbackName := "test:tokenprocessor_usage_insert_error"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		// 只阻断 usage_events 的事务写入，不能影响测试预先写入 Redis inbox。
		if tx.Statement == nil || tx.Statement.Table != "usage_events" {
			return
		}
		_ = tx.AddError(injectedErr)
	}); err != nil {
		t.Fatalf("register usage event insert callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })
}

func captureTokenProcessorLogsAtLevel(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	previousLevel := logger.Level
	logs := &bytes.Buffer{}
	logrus.SetOutput(logs)
	logrus.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	logrus.SetLevel(level)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return logs
}

type tokenProcessorLogEntry struct {
	Message              string         `json:"msg"`
	EventKey             string         `json:"event_key"`
	AttemptCount         int            `json:"attempt_count"`
	UnknownExecutorCount int            `json:"unknown_executor_count"`
	TokenReadyCount      int            `json:"token_ready_count"`
	OutcomeCounts        map[string]int `json:"token_outcome_counts"`
	ActionCounts         map[string]int `json:"token_action_counts"`
}

func decodeTokenProcessorLogEntries(t *testing.T, raw string) []tokenProcessorLogEntry {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	var entries []tokenProcessorLogEntry
	for {
		var entry tokenProcessorLogEntry
		if err := decoder.Decode(&entry); errors.Is(err, io.EOF) {
			return entries
		} else if err != nil {
			t.Fatalf("decode token log: %v", err)
		}
		entries = append(entries, entry)
	}
}

func findAllTokenProcessorLogEntries(entries []tokenProcessorLogEntry, message string) []tokenProcessorLogEntry {
	var matches []tokenProcessorLogEntry
	for _, entry := range entries {
		if entry.Message == message {
			matches = append(matches, entry)
		}
	}
	return matches
}

func tokenProcessorAttentionEventKeys(entries []tokenProcessorLogEntry) map[string]bool {
	keys := map[string]bool{}
	for _, entry := range findAllTokenProcessorLogEntries(entries, "redis usage token event requires attention") {
		keys[entry.EventKey] = true
	}
	return keys
}
