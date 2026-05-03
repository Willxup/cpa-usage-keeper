package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type stubMetadataFetcher struct {
	authFilesResult *cpa.AuthFilesResult
	authFilesErr    error
	providerConfig  cpa.ProviderMetadataConfig
	geminiErr       error
	claudeErr       error
	codexErr        error
	vertexErr       error
	openAIErr       error
	geminiNilResult bool
}

type trackingMetadataFetcher struct {
	authCalls   int
	geminiCalls int
	claudeCalls int
	codexCalls  int
	vertexCalls int
	openAICalls int
	authErr     error
	providerErr error
}

type observingMetadataFetcher struct {
	db                            *gorm.DB
	usageEventsBeforeMetadataSync int64
}

func (s stubMetadataFetcher) FetchAuthFiles(context.Context) (*cpa.AuthFilesResult, error) {
	if s.authFilesResult != nil || s.authFilesErr != nil {
		return s.authFilesResult, s.authFilesErr
	}
	return &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{}}, nil
}

func (s stubMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	if s.geminiNilResult {
		return nil, nil
	}
	return providerKeyConfigResult(s.providerConfig.GeminiAPIKeys, s.geminiErr)
}

func (s stubMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.ClaudeAPIKeys, s.claudeErr)
}

func (s stubMetadataFetcher) FetchCodexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.CodexAPIKeys, s.codexErr)
}

func (s stubMetadataFetcher) FetchVertexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.VertexAPIKeys, s.vertexErr)
}

func (s stubMetadataFetcher) FetchOpenAICompatibility(context.Context) (*cpa.OpenAICompatibilityResult, error) {
	return openAICompatibilityResult(s.providerConfig.OpenAICompatibility, s.openAIErr)
}

func providerKeyConfigResult(payload []cpa.ProviderKeyConfig, err error) (*cpa.ProviderKeyConfigResult, error) {
	if err != nil {
		return nil, err
	}
	return &cpa.ProviderKeyConfigResult{StatusCode: 200, Payload: payload}, nil
}

func openAICompatibilityResult(payload []cpa.OpenAICompatibilityConfig, err error) (*cpa.OpenAICompatibilityResult, error) {
	if err != nil {
		return nil, err
	}
	return &cpa.OpenAICompatibilityResult{StatusCode: 200, Payload: payload}, nil
}

func (s *trackingMetadataFetcher) FetchAuthFiles(context.Context) (*cpa.AuthFilesResult, error) {
	s.authCalls++
	if s.authErr != nil {
		return nil, s.authErr
	}
	return &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{}}, nil
}

func (s *trackingMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	s.geminiCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	s.claudeCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchCodexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	s.codexCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchVertexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	s.vertexCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchOpenAICompatibility(context.Context) (*cpa.OpenAICompatibilityResult, error) {
	s.openAICalls++
	return openAICompatibilityResult(nil, s.providerErr)
}

func (s *observingMetadataFetcher) FetchAuthFiles(context.Context) (*cpa.AuthFilesResult, error) {
	if err := s.db.Model(&models.UsageEvent{}).Count(&s.usageEventsBeforeMetadataSync).Error; err != nil {
		return nil, err
	}
	return &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{}}, nil
}

func (s *observingMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchCodexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchVertexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchOpenAICompatibility(context.Context) (*cpa.OpenAICompatibilityResult, error) {
	return openAICompatibilityResult(nil, nil)
}

func (s *trackingMetadataFetcher) providerCalls() int {
	return s.geminiCalls + s.claudeCalls + s.codexCalls + s.vertexCalls + s.openAICalls
}

func TestPullRedisUsageInboxOnlyStoresPendingRows(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"pull-only","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
	})

	result, err := service.PullRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("PullRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Empty || result.Status != "completed" || result.InsertedRows != 1 {
		t.Fatalf("unexpected pull result: %+v", result)
	}

	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusPending || inbox.UsageEventKey != "" {
		t.Fatalf("expected pending inbox row without processing links, got %+v", inbox)
	}
	var eventCount int64
	if err := db.Model(&models.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected pull not to write usage events, got %d", eventCount)
	}
}

func TestProcessRedisUsageInboxPersistsEventsWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repository.RedisInboxInsert{{
		QueueKey:   cpa.ManagementUsageQueueKey,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","endpoint":"/v1/messages","auth_type":"api_key","model":"sonnet","request_id":"process-only","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("redis should not be popped while processing inbox")},
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "process-only" {
		t.Fatalf("expected Redis event without snapshot run id, got %+v", event)
	}
	if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "process-only" {
		t.Fatalf("expected Redis identity fields to persist, got %+v", event)
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "process-only" {
		t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
	}
}

func TestProcessRedisUsageInboxDoesNotFetchMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repository.RedisInboxInsert{{
		QueueKey:   cpa.ManagementUsageQueueKey,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-no-metadata","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: metadata,
	})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected redis processing not to fetch metadata, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-no-metadata" {
		t.Fatalf("expected inbox row processed, got %+v", inbox)
	}
}

func TestSyncRedisBatchSkipsEmptyBatchWithoutSnapshotOrMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		RedisQueue:      staticRedisQueue{},
		MetadataFetcher: metadata,
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("expected empty redis batch result, got %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped for empty batch, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

}

func TestSyncRedisBatchPersistsNonEmptyBatchWithoutMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		RedisQueue:      staticRedisQueue{messages: []string{`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-1","tokens":{"input_tokens":1,"output_tokens":2}}`}},
		MetadataFetcher: metadata,
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || result.Empty || result.Status != "completed" || result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("unexpected redis batch result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-1" {
		t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
	}
}

func TestSyncRedisBatchPersistsValidRowsWhenBatchContainsMalformedMessage(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-valid","tokens":{"input_tokens":1,"output_tokens":2}}`,
			`{bad-json}`,
		}},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" || result.InsertedEvents != 1 {
		t.Fatalf("expected warning result with valid event persisted, got %+v", result)
	}

	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-valid" {
		t.Fatalf("unexpected usage event: %+v", event)
	}

	var inboxRows []models.RedisUsageInbox
	if err := db.Order("id asc").Find(&inboxRows).Error; err != nil {
		t.Fatalf("load inbox rows: %v", err)
	}
	if len(inboxRows) != 2 {
		t.Fatalf("expected 2 inbox rows, got %d", len(inboxRows))
	}
	if inboxRows[0].Status != repository.RedisUsageInboxStatusProcessed || inboxRows[0].UsageEventKey != "redis-valid" {
		t.Fatalf("expected first row processed, got %+v", inboxRows[0])
	}
	if inboxRows[1].Status != repository.RedisUsageInboxStatusDecodeFailed || inboxRows[1].LastError == "" {
		t.Fatalf("expected second row decode_failed, got %+v", inboxRows[1])
	}
}

func TestSyncRedisBatchMarksMalformedOnlyBatchWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{`{bad-json}`}},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || inbox.RawMessage != `{bad-json}` {
		t.Fatalf("expected decode_failed raw inbox row, got %+v", inbox)
	}
}

func TestSyncRedisBatchProcessesPendingInboxBeforePoppingRedis(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repository.RedisInboxInsert{{
		QueueKey:   cpa.ManagementUsageQueueKey,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"pending-1","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   poppedAt,
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("redis should not be popped while inbox is pending")},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("expected pending inbox row to be processed, got %+v", result)
	}

	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "pending-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected pending row processed, got %+v", inbox)
	}
}

func TestSyncRedisBatchDoesNotWatermarkFilterRedisInboxEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{{
		EventKey:    "future-watermark",
		APIGroupKey: "claude",
		Model:       "sonnet",
		Timestamp:   time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed future event: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-26T07:00:00Z","provider":"claude","model":"sonnet","request_id":"old-but-unique","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected old unique Redis event to insert despite watermark, got %+v", result)
	}

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "old-but-unique").First(&event).Error; err != nil {
		t.Fatalf("load old unique Redis event: %v", err)
	}
}

func TestSyncRedisBatchRetriesProcessFailedInboxBeforePoppingRedis(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []repository.RedisInboxInsert{{
		QueueKey:   cpa.ManagementUsageQueueKey,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"retry-process-failed","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   poppedAt,
	}})
	if err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := repository.MarkRedisUsageInboxProcessFailed(db, rows[0].ID, errors.New("temporary insert failure")); err != nil {
		t.Fatalf("mark process failed: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("redis should not be popped while process_failed inbox is retryable")},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected process_failed row retry to insert, got %+v", result)
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.LastError != "" {
		t.Fatalf("expected retried row processed and error cleared, got %+v", inbox)
	}
}

func TestSyncNowInRedisModeUsesDurableInbox(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"sync-now-redis","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
		MetadataFetcher: metadata,
	})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected SyncNow result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected SyncNow not to fetch metadata, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "sync-now-redis" {
		t.Fatalf("expected SyncNow redis path to use inbox, got %+v", inbox)
	}
}

func TestSyncRedisBatchKeepsRedisRequestIDWhenEquivalentCanonicalEventExists(t *testing.T) {
	db := openSyncTestDatabase(t)
	timestamp := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	tokens := cpa.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 4, TotalTokens: 39}
	canonicalKey := BuildEventKey("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, tokens)
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{{
		EventKey:        canonicalKey,
		APIGroupKey:     "external-api-key",
		Model:           "claude-sonnet",
		Timestamp:       timestamp,
		Source:          "codex-a",
		AuthIndex:       "1",
		Failed:          false,
		LatencyMS:       123,
		InputTokens:     tokens.InputTokens,
		OutputTokens:    tokens.OutputTokens,
		ReasoningTokens: tokens.ReasoningTokens,
		CachedTokens:    tokens.CachedTokens,
		TotalTokens:     tokens.TotalTokens,
	}}); err != nil {
		t.Fatalf("seed canonical usage event: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-canonical"),
		}},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("expected Redis request_id event to insert separately from canonical event, got %+v", result)
	}
	assertUsageEventCount(t, db, 2)
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.UsageEventKey != "redis-request-canonical" {
		t.Fatalf("expected inbox to keep Redis request_id event key, got %+v", inbox)
	}
}

func TestSyncRedisBatchKeepsDistinctRedisRequestIDsWithSameCanonicalFields(t *testing.T) {
	db := openSyncTestDatabase(t)
	timestamp := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	tokens := cpa.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 4, TotalTokens: 39}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-1"),
			equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-2"),
		}},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result.InsertedEvents != 2 || result.DedupedEvents != 0 {
		t.Fatalf("expected distinct Redis request IDs to insert separately, got %+v", result)
	}
	assertUsageEventCount(t, db, 2)
}

func TestSyncRedisBatchWritesDebugLogsWithoutRawPayload(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)

	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-log","api_key":"raw-secret-key","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
	})

	_, err := service.SyncRedisBatch(context.Background())
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	output := logs.String()
	for _, expected := range []string{
		"redis usage batch popped",
		"redis usage inbox rows inserted",
		"redis usage inbox rows processed",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected debug log %q in output:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "raw-secret-key") || strings.Contains(output, "redis-log") {
		t.Fatalf("debug logs should not include raw payload fields, got:\n%s", output)
	}
}

func TestSyncMetadataRefreshesMetadataWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: metadata,
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	if metadata.authCalls != 1 || metadata.providerCalls() != 5 {
		t.Fatalf("expected metadata fetch once, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}
}

func TestSyncMetadataWritesAuthFilesToUsageIdentities(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{Files: []cpa.AuthFile{{
			AuthIndex: "auth-1",
			Name:      "Fallback Name",
			Email:     "user@example.com",
			Type:      "claude",
			Provider:  "Claude",
			Label:     "Label Name",
		}, {
			AuthIndex: "auth-2",
			Name:      "Name Fallback",
			Type:      "gemini",
			Provider:  "Gemini",
			Label:     "Label Fallback",
		}, {
			AuthIndex: "auth-3",
			Name:      "Name Fallback",
			Type:      "codex",
			Provider:  "Codex",
		}, {
			AuthIndex: "auth-4",
			Type:      "vertex",
			Provider:  "Vertex",
		}}}}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	items, err := repository.ListUsageIdentities(context.Background(), db)
	if err != nil {
		t.Fatalf("list usage identities: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	first := byIdentity["auth-1"]
	if first.Name != "user@example.com" || first.AuthType != models.UsageIdentityAuthTypeAuthFile || first.AuthTypeName != "oauth" || first.Identity != "auth-1" || first.Type != "claude" || first.Provider != "Claude" || first.IsDeleted {
		t.Fatalf("unexpected auth usage identity for auth-1: %+v", first)
	}
	second := byIdentity["auth-2"]
	if second.Name != "Label Fallback" || second.AuthTypeName != "oauth" || second.Identity != "auth-2" || second.Type != "gemini" || second.Provider != "Gemini" || second.IsDeleted {
		t.Fatalf("unexpected auth usage identity for auth-2: %+v", second)
	}
	third := byIdentity["auth-3"]
	if third.Name != "Name Fallback" || third.AuthTypeName != "oauth" || third.Identity != "auth-3" || third.Type != "codex" || third.Provider != "Codex" || third.IsDeleted {
		t.Fatalf("unexpected auth usage identity for auth-3: %+v", third)
	}
	fourth := byIdentity["auth-4"]
	if fourth.Name != "auth-4" || fourth.AuthTypeName != "oauth" || fourth.Identity != "auth-4" || fourth.Type != "vertex" || fourth.Provider != "Vertex" || fourth.IsDeleted {
		t.Fatalf("unexpected auth usage identity for auth-4: %+v", fourth)
	}
	assertTableNotExists(t, db, "auth_files")
}

func TestSyncMetadataWritesProviderMetadataToUsageIdentities(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: cpa.ProviderMetadataConfig{
			ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude Team"}},
		}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	items, err := repository.ListUsageIdentities(context.Background(), db)
	if err != nil {
		t.Fatalf("list usage identities: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	apiKey := byIdentity["claude-key"]
	if apiKey.Name != "Claude Team" || apiKey.AuthType != models.UsageIdentityAuthTypeAIProvider || apiKey.AuthTypeName != "apikey" || apiKey.Identity != "claude-key" || apiKey.Type != "claude" || apiKey.Provider != "Claude Team" || apiKey.IsDeleted {
		t.Fatalf("unexpected provider usage identity for api key: %+v", apiKey)
	}
	prefix := byIdentity["claude-prefix"]
	if prefix.Name != "Claude Team" || prefix.AuthTypeName != "apikey" || prefix.Identity != "claude-prefix" || prefix.Type != "claude" || prefix.Provider != "Claude Team" || prefix.IsDeleted {
		t.Fatalf("unexpected provider usage identity for prefix: %+v", prefix)
	}
	assertTableNotExists(t, db, "provider_metadata")
}

func TestSyncMetadataDoesNotUseOpenAICompatibilityPrefixAsDisplayName(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: cpa.ProviderMetadataConfig{
			OpenAICompatibility: []cpa.OpenAICompatibilityConfig{{
				Prefix:        "https://proxy.internal/v1",
				APIKeyEntries: []cpa.OpenAIApiKeyEntry{{APIKey: "openai-compatible-key"}},
			}},
		}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	items, err := repository.ListUsageIdentities(context.Background(), db)
	if err != nil {
		t.Fatalf("list usage identities: %v", err)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	for _, identityKey := range []string{"https://proxy.internal/v1", "openai-compatible-key"} {
		identity := byIdentity[identityKey]
		if identity.Identity != identityKey {
			t.Fatalf("expected usage identity %q, got %+v", identityKey, identity)
		}
		if identity.Name != "openai" || identity.Provider != "openai" {
			t.Fatalf("expected raw OpenAI compatibility prefix not to be used as display value, got %+v", identity)
		}
	}
}

func TestSyncMetadataUsageIdentityPartialFailureKeepsFailedProviderType(t *testing.T) {
	db := openSyncTestDatabase(t)
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	oldDeletedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&[]models.UsageIdentity{{
		Name:         "Old Gemini",
		AuthType:     models.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-gemini-key",
		Type:         "gemini",
		Provider:     "Old Gemini",
	}, {
		Name:         "Old Claude",
		AuthType:     models.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-claude-key",
		Type:         "claude",
		Provider:     "Old Claude",
		DeletedAt:    &oldDeletedAt,
	}}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		Now:     func() time.Time { return now },
		MetadataFetcher: stubMetadataFetcher{
			providerConfig: cpa.ProviderMetadataConfig{ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "new-claude-key", Prefix: "new-claude-prefix", Name: "New Claude"}}},
			geminiErr:      errors.New("gemini unavailable"),
		},
	})

	err := service.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gemini unavailable") {
		t.Fatalf("expected provider metadata warning, got %v", err)
	}
	items, listErr := repository.ListUsageIdentities(context.Background(), db)
	if listErr != nil {
		t.Fatalf("list usage identities: %v", listErr)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	if oldGemini := byIdentity["old-gemini-key"]; oldGemini.Identity == "" || oldGemini.IsDeleted || oldGemini.DeletedAt != nil {
		t.Fatalf("expected failed gemini identity to remain untouched, got %+v", oldGemini)
	}
	if oldClaude := byIdentity["old-claude-key"]; oldClaude.Identity == "" || !oldClaude.IsDeleted || oldClaude.DeletedAt == nil || !oldClaude.DeletedAt.Equal(now) {
		t.Fatalf("expected stale successful claude identity to be deleted at sync time, got %+v", oldClaude)
	}
	if newClaude := byIdentity["new-claude-key"]; newClaude.Identity == "" || newClaude.IsDeleted {
		t.Fatalf("expected new claude identity to be active, got %+v", newClaude)
	}
}

func TestSyncMetadataAggregatesUsageIdentityStatsAfterUpsert(t *testing.T) {
	db := openSyncTestDatabase(t)
	eventTime := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{{
		EventKey:     "auth-stat-event",
		AuthType:     "oauth",
		AuthIndex:    "auth-stat",
		Model:        "sonnet",
		Timestamp:    eventTime,
		InputTokens:  11,
		OutputTokens: 13,
		TotalTokens:  24,
	}}); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		Now:     func() time.Time { return now },
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{Files: []cpa.AuthFile{{
			AuthIndex: "auth-stat",
			Email:     "stats@example.com",
			Type:      "claude",
			Provider:  "Claude",
		}}}}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	var identity models.UsageIdentity
	if err := db.Where("identity = ?", "auth-stat").First(&identity).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	if identity.TotalRequests != 1 || identity.SuccessCount != 1 || identity.InputTokens != 11 || identity.OutputTokens != 13 || identity.TotalTokens != 24 || identity.LastAggregatedUsageEventID == 0 || identity.StatsUpdatedAt == nil || !identity.StatsUpdatedAt.Equal(now) {
		t.Fatalf("expected usage identity stats aggregated after metadata upsert, got %+v", identity)
	}
	if identity.FirstUsedAt == nil || !identity.FirstUsedAt.Equal(eventTime) || identity.LastUsedAt == nil || !identity.LastUsedAt.Equal(eventTime) {
		t.Fatalf("expected usage identity first/last usage times from seeded event, got %+v", identity)
	}
}

func TestSyncMetadataPersistsProviderUsageIdentitiesFromDedicatedEndpoints(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: cpa.ProviderMetadataConfig{
			GeminiAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "gemini-key", Prefix: "gemini-prefix", Name: "Gemini"}},
			ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude"}},
			OpenAICompatibility: []cpa.OpenAICompatibilityConfig{{
				Name:          "Custom OpenAI",
				Prefix:        "custom-openai",
				APIKeyEntries: []cpa.OpenAIApiKeyEntry{{APIKey: "custom-key"}},
			}},
		}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	items, err := repository.ListUsageIdentities(context.Background(), db)
	if err != nil {
		t.Fatalf("list usage identities: %v", err)
	}
	providerItems := usageIdentitiesByIdentity(items)
	for _, expected := range []string{"gemini-key", "gemini-prefix", "claude-key", "claude-prefix", "custom-key", "custom-openai"} {
		identity := providerItems[expected]
		if identity.Identity != expected || identity.AuthType != models.UsageIdentityAuthTypeAIProvider || identity.AuthTypeName != "apikey" || identity.IsDeleted {
			t.Fatalf("expected active provider usage identity %q, got %+v", expected, identity)
		}
	}
	assertTableNotExists(t, db, "provider_metadata")
}

func TestSyncMetadataPersistsSuccessfulProviderUsageIdentitiesWhenOneEndpointFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{
			providerConfig: cpa.ProviderMetadataConfig{ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude"}}},
			geminiErr:      errors.New("gemini unavailable"),
		},
	})

	err := service.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gemini unavailable") {
		t.Fatalf("expected provider metadata warning, got %v", err)
	}
	items, listErr := repository.ListUsageIdentities(context.Background(), db)
	if listErr != nil {
		t.Fatalf("list usage identities: %v", listErr)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	for _, expected := range []string{"claude-key", "claude-prefix"} {
		identity := byIdentity[expected]
		if identity.Identity != expected || identity.Type != "claude" || identity.AuthType != models.UsageIdentityAuthTypeAIProvider || identity.IsDeleted {
			t.Fatalf("expected successful provider usage identity %q to persist, got %+v", expected, identity)
		}
	}
	if _, ok := byIdentity["gemini-key"]; ok {
		t.Fatalf("expected failed gemini endpoint not to create usage identity, got %+v", items)
	}
}

func TestSyncMetadataKeepsFailedProviderUsageIdentitiesDuringPartialFailure(t *testing.T) {
	db := openSyncTestDatabase(t)
	now := time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)
	if err := db.Create(&[]models.UsageIdentity{{
		Name:         "Old Gemini",
		AuthType:     models.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-gemini-key",
		Type:         "gemini",
		Provider:     "Old Gemini",
	}, {
		Name:         "Old Claude",
		AuthType:     models.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-claude-key",
		Type:         "claude",
		Provider:     "Old Claude",
	}}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		Now:     func() time.Time { return now },
		MetadataFetcher: stubMetadataFetcher{
			providerConfig: cpa.ProviderMetadataConfig{ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "new-claude-key", Prefix: "new-claude-prefix", Name: "New Claude"}}},
			geminiErr:      errors.New("gemini unavailable"),
		},
	})

	err := service.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gemini unavailable") {
		t.Fatalf("expected provider metadata warning, got %v", err)
	}
	items, listErr := repository.ListUsageIdentities(context.Background(), db)
	if listErr != nil {
		t.Fatalf("list usage identities: %v", listErr)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	if oldGemini := byIdentity["old-gemini-key"]; oldGemini.Identity == "" || oldGemini.IsDeleted || oldGemini.DeletedAt != nil {
		t.Fatalf("expected failed gemini usage identity to remain untouched, got %+v", oldGemini)
	}
	if oldClaude := byIdentity["old-claude-key"]; oldClaude.Identity == "" || !oldClaude.IsDeleted || oldClaude.DeletedAt == nil || !oldClaude.DeletedAt.Equal(now) {
		t.Fatalf("expected stale successful claude usage identity to be deleted, got %+v", oldClaude)
	}
	for _, expected := range []string{"new-claude-key", "new-claude-prefix"} {
		identity := byIdentity[expected]
		if identity.Identity != expected || identity.IsDeleted {
			t.Fatalf("expected active replacement usage identity %q, got %+v", expected, identity)
		}
	}
}

func TestSyncMetadataKeepsProviderUsageIdentitiesWhenEndpointReturnsNilResult(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := db.Create(&models.UsageIdentity{
		Name:         "Old Gemini",
		AuthType:     models.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-gemini-key",
		Type:         "gemini",
		Provider:     "Old Gemini",
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{geminiNilResult: true},
	})

	err := service.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gemini api keys response is nil") {
		t.Fatalf("expected nil gemini response warning, got %v", err)
	}
	items, listErr := repository.ListUsageIdentities(context.Background(), db)
	if listErr != nil {
		t.Fatalf("list usage identities: %v", listErr)
	}
	byIdentity := usageIdentitiesByIdentity(items)
	oldGemini := byIdentity["old-gemini-key"]
	if oldGemini.Identity == "" || oldGemini.IsDeleted || oldGemini.DeletedAt != nil {
		t.Fatalf("expected old gemini usage identity to remain, got %+v", oldGemini)
	}
}

func TestSyncRedisBatchErrorDoesNotCreateSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("dial failed")},
	})

	result, err := service.SyncRedisBatch(context.Background())
	if err == nil || result == nil || result.Status != "failed" {
		t.Fatalf("expected failed redis batch result, got result=%+v err=%v", result, err)
	}
}

func TestNewSyncServiceBuildsClientFromConfig(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncService(db, config.Config{
		CPABaseURL:       " https://cpa.example.com ",
		CPAManagementKey: "secret",
		RequestTimeout:   5 * time.Second,
	})
	if service == nil || service.client == nil {
		t.Fatal("expected sync service client to be initialized")
	}
	if service.baseURL != "https://cpa.example.com" {
		t.Fatalf("expected trimmed base url, got %q", service.baseURL)
	}
}

func equivalentRedisMessage(apiGroupKey, model string, timestamp time.Time, source, authIndex string, failed bool, latencyMS int64, tokens cpa.TokenStats, requestID string) string {
	failedValue := "false"
	if failed {
		failedValue = "true"
	}
	return `{"timestamp":"` + timestamp.UTC().Format(time.RFC3339) + `","latency_ms":` + int64String(latencyMS) + `,"source":"` + source + `","auth_index":"` + authIndex + `","failed":` + failedValue + `,"api_key":"` + apiGroupKey + `","model":"` + model + `","request_id":"` + requestID + `","tokens":{"input_tokens":` + int64String(tokens.InputTokens) + `,"output_tokens":` + int64String(tokens.OutputTokens) + `,"reasoning_tokens":` + int64String(tokens.ReasoningTokens) + `,"cached_tokens":` + int64String(tokens.CachedTokens) + `,"total_tokens":` + int64String(tokens.TotalTokens) + `}}`
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

func usageIdentitiesByIdentity(items []models.UsageIdentity) map[string]models.UsageIdentity {
	byIdentity := make(map[string]models.UsageIdentity, len(items))
	for _, item := range items {
		byIdentity[item.Identity] = item
	}
	return byIdentity
}

func assertUsageEventCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != expected {
		t.Fatalf("expected %d usage events, got %d", expected, count)
	}
}

func assertTableNotExists(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	if db.Migrator().HasTable(table) {
		t.Fatalf("expected %s table not to exist", table)
	}
}

func openSyncTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "sync.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}

func closeTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
}

func captureSyncDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	logs := &bytes.Buffer{}
	previousOutput := logrus.StandardLogger().Out
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(logs)
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetLevel(previousLevel)
	})
	return logs
}

func openSyncTestDatabaseWithLogs(t *testing.T) (*gorm.DB, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	gormLogger := gormlogger.New(
		log.New(logs, "", 0),
		gormlogger.Config{
			LogLevel:                  gormlogger.Info,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sync.db")), &gorm.Config{Logger: gormLogger})
	if err != nil {
		t.Fatalf("gorm.Open returned error: %v", err)
	}
	closeTestDatabase(t, db)
	if err := db.AutoMigrate(models.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}
	return db, logs
}
