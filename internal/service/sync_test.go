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
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type stubMetadataFetcher struct {
	authFilesResult *response.AuthFilesResult
	authFilesErr    error
	apiKeysResult   *response.ManagementAPIKeysResult
	apiKeysErr      error
	providerConfig  providerconfig.ProviderMetadataConfig
	geminiErr       error
	claudeErr       error
	codexErr        error
	vertexErr       error
	openAIErr       error
	geminiNilResult bool
}

type trackingMetadataFetcher struct {
	authCalls   int
	apiKeyCalls int
	geminiCalls int
	claudeCalls int
	codexCalls  int
	vertexCalls int
	openAICalls int
	authErr     error
	apiKeysErr  error
	providerErr error
}

type observingMetadataFetcher struct {
	db                            *gorm.DB
	usageEventsBeforeMetadataSync int64
}

func (s stubMetadataFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	if s.authFilesResult != nil || s.authFilesErr != nil {
		return s.authFilesResult, s.authFilesErr
	}
	return &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{}}, nil
}

func (s stubMetadataFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	if s.apiKeysResult != nil || s.apiKeysErr != nil {
		return s.apiKeysResult, s.apiKeysErr
	}
	return &response.ManagementAPIKeysResult{StatusCode: 200}, nil
}

func (s stubMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	if s.geminiNilResult {
		return nil, nil
	}
	return providerKeyConfigResult(s.providerConfig.GeminiAPIKeys, s.geminiErr)
}

func (s stubMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.ClaudeAPIKeys, s.claudeErr)
}

func (s stubMetadataFetcher) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.CodexAPIKeys, s.codexErr)
}

func (s stubMetadataFetcher) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.VertexAPIKeys, s.vertexErr)
}

func (s stubMetadataFetcher) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	return openAICompatibilityResult(s.providerConfig.OpenAICompatibility, s.openAIErr)
}

func providerKeyConfigResult(payload []providerconfig.ProviderKeyConfig, err error) (*response.ProviderKeyConfigResult, error) {
	if err != nil {
		return nil, err
	}
	return &response.ProviderKeyConfigResult{StatusCode: 200, Payload: payload}, nil
}

func openAICompatibilityResult(payload []providerconfig.OpenAICompatibilityConfig, err error) (*response.OpenAICompatibilityResult, error) {
	if err != nil {
		return nil, err
	}
	return &response.OpenAICompatibilityResult{StatusCode: 200, Payload: payload}, nil
}

func (s *trackingMetadataFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	s.authCalls++
	if s.authErr != nil {
		return nil, s.authErr
	}
	return &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{}}, nil
}

func (s *trackingMetadataFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	s.apiKeyCalls++
	if s.apiKeysErr != nil {
		return nil, s.apiKeysErr
	}
	return &response.ManagementAPIKeysResult{StatusCode: 200}, nil
}

func (s *trackingMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	s.geminiCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	s.claudeCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	s.codexCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	s.vertexCalls++
	return providerKeyConfigResult(nil, s.providerErr)
}

func (s *trackingMetadataFetcher) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	s.openAICalls++
	return openAICompatibilityResult(nil, s.providerErr)
}

func (s *observingMetadataFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	if err := s.db.Model(&entities.UsageEvent{}).Count(&s.usageEventsBeforeMetadataSync).Error; err != nil {
		return nil, err
	}
	return &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{}}, nil
}

func (s *observingMetadataFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	return &response.ManagementAPIKeysResult{StatusCode: 200}, nil
}

func (s *observingMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(nil, nil)
}

func (s *observingMetadataFetcher) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
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

	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusPending || inbox.UsageEventKey != "" {
		t.Fatalf("expected pending inbox row without processing links, got %+v", inbox)
	}
	var eventCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected pull not to write usage events, got %d", eventCount)
	}
}

func TestProcessRedisUsageInboxPersistsEventsWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
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
	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "process-only" {
		t.Fatalf("expected Redis event without snapshot run id, got %+v", event)
	}
	if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "process-only" {
		t.Fatalf("expected Redis identity fields to persist, got %+v", event)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "process-only" {
		t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
	}
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("expected overview aggregation checkpoint after processing inbox: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != event.ID {
		t.Fatalf("expected overview checkpoint to aggregate through event %d, got %+v", event.ID, checkpoint)
	}
}

func TestProcessRedisUsageInboxRollsBackEventsWhenProcessedMarkFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		QueueKey:   cpa.ManagementUsageQueueKey,
		RawMessage: `{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"rollback-on-mark-failure","tokens":{"input_tokens":1,"output_tokens":2}}`,
		PoppedAt:   time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_processed_mark BEFORE UPDATE OF status ON redis_usage_inboxes WHEN NEW.status = 'processed' BEGIN SELECT RAISE(ABORT, 'processed mark failed'); END;`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	_, err := service.ProcessRedisUsageInbox(context.Background())
	if err == nil || !strings.Contains(err.Error(), "processed mark failed") {
		t.Fatalf("expected processed mark failure, got %v", err)
	}
	var eventCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected usage event insert to roll back when inbox mark fails, got %d", eventCount)
	}
}

func TestProcessRedisUsageInboxSkipsAggregationWhenInboxAndEventsAreEmpty(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var checkpointCount int64
	if err := db.Model(&entities.UsageOverviewAggregationCheckpoint{}).Where("name = ?", "overview").Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview aggregation checkpoint: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected empty process without usage events not to create aggregation checkpoint, got %d", checkpointCount)
	}
}

func TestProcessRedisUsageInboxRetriesOverviewAggregationWhenInboxIsEmpty(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "stale-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC), TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{BaseURL: "https://cpa.example.com"})

	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("unexpected empty process result: %+v", result)
	}
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("expected overview aggregation checkpoint after empty process catch-up: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected empty process catch-up to aggregate stale usage events, got %+v", checkpoint)
	}
}

func TestProcessRedisUsageInboxDoesNotFetchMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
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
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-no-metadata" {
		t.Fatalf("expected inbox row processed, got %+v", inbox)
	}
}

func processPendingOrPulledRedisUsageForTest(t *testing.T, service *SyncService) (*servicedto.RedisBatchSyncResult, error) {
	t.Helper()
	result, err := service.ProcessRedisUsageInbox(context.Background())
	if err != nil || result == nil || !result.Empty {
		return result, err
	}
	if _, err := service.PullRedisUsageInbox(context.Background()); err != nil {
		return &servicedto.RedisBatchSyncResult{Status: "failed"}, err
	}
	return service.ProcessRedisUsageInbox(context.Background())
}

func TestSplitRedisUsageSyncSkipsEmptyBatchWithoutSnapshotOrMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		RedisQueue:      staticRedisQueue{},
		MetadataFetcher: metadata,
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("expected empty redis batch result, got %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped for empty batch, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

}

func TestSplitRedisUsageSyncPersistsNonEmptyBatchWithoutMetadata(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		RedisQueue:      staticRedisQueue{messages: []string{`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-1","tokens":{"input_tokens":1,"output_tokens":2}}`}},
		MetadataFetcher: metadata,
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || result.Empty || result.Status != "completed" || result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("unexpected redis batch result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "redis-1" {
		t.Fatalf("expected processed inbox row without snapshot link, got %+v", inbox)
	}
}

func TestSplitRedisUsageSyncPersistsValidRowsWhenBatchContainsMalformedMessage(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-valid","tokens":{"input_tokens":1,"output_tokens":2}}`,
			`{bad-json}`,
		}},
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" || result.InsertedEvents != 1 {
		t.Fatalf("expected warning result with valid event persisted, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-valid" {
		t.Fatalf("unexpected usage event: %+v", event)
	}

	var inboxRows []entities.RedisUsageInbox
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

func TestSplitRedisUsageSyncMarksMalformedOnlyBatchWithoutSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{`{bad-json}`}},
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || inbox.RawMessage != `{bad-json}` {
		t.Fatalf("expected decode_failed raw inbox row, got %+v", inbox)
	}
}

func TestSplitRedisUsageSyncLogsErrorAndMarksDecodeFailedWhenRequestIDMissing(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","tokens":{"input_tokens":1,"output_tokens":2}}`}},
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err == nil || !strings.Contains(err.Error(), "request_id is required") {
		t.Fatalf("expected missing request_id warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusDecodeFailed || !strings.Contains(inbox.LastError, "request_id is required") {
		t.Fatalf("expected missing request_id decode_failed row, got %+v", inbox)
	}
	output := logs.String()
	if !strings.Contains(output, "level=error") || !strings.Contains(output, "redis usage message decode failed") || !strings.Contains(output, "request_id is required") {
		t.Fatalf("expected missing request_id error log, got:\n%s", output)
	}
}

func TestSplitRedisUsageSyncProcessesPendingInboxBeforePoppingRedis(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
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

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("expected pending inbox row to be processed, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "pending-1" {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected pending row processed, got %+v", inbox)
	}
}

func TestSplitRedisUsageSyncDoesNotWatermarkFilterRedisInboxEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
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

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected old unique Redis event to insert despite watermark, got %+v", result)
	}

	var event entities.UsageEvent
	if err := db.Where("event_key = ?", "old-but-unique").First(&event).Error; err != nil {
		t.Fatalf("load old unique Redis event: %v", err)
	}
}

func TestSplitRedisUsageSyncRetriesProcessFailedInboxBeforePoppingRedis(t *testing.T) {
	db := openSyncTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	rows, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
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

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("expected process_failed row retry to insert, got %+v", result)
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.LastError != "" {
		t.Fatalf("expected retried row processed and error cleared, got %+v", inbox)
	}
}

func TestSplitRedisUsageSyncUsesDurableInbox(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"sync-now-redis","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
		MetadataFetcher: metadata,
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result == nil || result.InsertedEvents != 1 {
		t.Fatalf("unexpected split Redis usage sync result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected split Redis usage sync not to fetch metadata, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}
	var inbox entities.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "sync-now-redis" {
		t.Fatalf("expected split Redis usage sync redis path to use inbox, got %+v", inbox)
	}
}

func TestSplitRedisUsageSyncKeepsDistinctRedisRequestIDsWithSameEventFields(t *testing.T) {
	db := openSyncTestDatabase(t)
	timestamp := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	tokens := dto.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 4, TotalTokens: 39}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-1"),
			equivalentRedisMessage("external-api-key", "claude-sonnet", timestamp, "codex-a", "1", false, 123, tokens, "redis-request-2"),
		}},
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
	}
	if result.InsertedEvents != 2 || result.DedupedEvents != 0 {
		t.Fatalf("expected distinct Redis request IDs to insert separately, got %+v", result)
	}
	assertUsageEventCount(t, db, 2)
}

func TestSplitRedisUsageSyncWritesDebugLogsWithoutRawPayload(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)

	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-log","api_key":"raw-secret-key","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
	})

	_, err := processPendingOrPulledRedisUsageForTest(t, service)
	if err != nil {
		t.Fatalf("split Redis usage sync returned error: %v", err)
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
	if metadata.authCalls != 1 || metadata.apiKeyCalls != 1 || metadata.providerCalls() != 5 {
		t.Fatalf("expected metadata fetch once, got auth=%d apiKeys=%d provider=%d", metadata.authCalls, metadata.apiKeyCalls, metadata.providerCalls())
	}
}

func TestSyncMetadataWritesCPAAPIKeys(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{apiKeysResult: &response.ManagementAPIKeysResult{
			StatusCode: 200,
			Payload:    cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-alpha123456", "sk-beta654321"}},
		}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}

	rows, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 2 || rows[0].DisplayKey != "sk-*********123456" || rows[0].KeyAlias != "" {
		t.Fatalf("unexpected synced API key rows: %+v", rows)
	}
}

func TestSyncMetadataAPIKeyFetchFailureDoesNotDeleteLocalKeys(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{apiKeysErr: errors.New("management unavailable")},
	})

	if err := service.SyncMetadata(context.Background()); err == nil || !strings.Contains(err.Error(), "management unavailable") {
		t.Fatalf("expected API key fetch warning, got %v", err)
	}

	rows, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].APIKey != "sk-alpha123456" {
		t.Fatalf("expected existing key to remain active after fetch failure, got %+v", rows)
	}
}

func TestSyncMetadataWritesAuthFilesToUsageIdentities(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{{
			AuthIndex: "auth-1",
			Name:      "Fallback Name",
			Email:     "user@example.com",
			Type:      "claude",
			Provider:  "Claude",
			Label:     "Label Name",
			Prefix:    "auth-prefix",
			Priority:  intPtr(6),
			Disabled:  boolPtr(false),
			Note:      strPtr("auth note"),
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
	if first.Name != "user@example.com" || first.AuthType != entities.UsageIdentityAuthTypeAuthFile || first.AuthTypeName != "oauth" || first.Identity != "auth-1" || first.Type != "claude" || first.Provider != "Claude" || first.IsDeleted {
		t.Fatalf("unexpected auth usage identity for auth-1: %+v", first)
	}
	if first.Prefix != "auth-prefix" || first.Priority == nil || *first.Priority != 6 || first.Disabled == nil || *first.Disabled || first.Note == nil || *first.Note != "auth note" {
		t.Fatalf("expected auth sync metadata to persist, got %+v", first)
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

func TestSyncMetadataWritesCodexAuthFileIDTokenFieldsOnlyForCodex(t *testing.T) {
	db := openSyncTestDatabase(t)
	activeStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	activeUntil := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	accountID := "acct_123"
	ignoredAccountID := "acct_should_ignore"
	planType := "team"
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{{
			AuthIndex: "codex-auth",
			Email:     "codex@example.com",
			Type:      "codex",
			Provider:  "Codex",
			IDToken: &authfiles.AuthFileIDToken{
				AccountID:   &accountID,
				ActiveStart: &activeStart,
				ActiveUntil: &activeUntil,
				PlanType:    &planType,
			},
		}, {
			AuthIndex: "claude-auth",
			Email:     "claude@example.com",
			Type:      "claude",
			Provider:  "Claude",
			IDToken: &authfiles.AuthFileIDToken{
				AccountID:   &ignoredAccountID,
				ActiveStart: &activeStart,
				ActiveUntil: &activeUntil,
				PlanType:    &planType,
			},
		}, {
			AuthIndex: "codex-no-token",
			Email:     "codex-no-token@example.com",
			Type:      "codex",
			Provider:  "Codex",
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
	codex := byIdentity["codex-auth"]
	if codex.AccountID == nil || *codex.AccountID != "acct_123" || codex.PlanType == nil || *codex.PlanType != "team" || codex.ActiveStart == nil || !codex.ActiveStart.Equal(activeStart) || codex.ActiveUntil == nil || !codex.ActiveUntil.Equal(activeUntil) {
		t.Fatalf("expected codex id_token fields to persist, got %+v", codex)
	}
	claude := byIdentity["claude-auth"]
	if claude.AccountID != nil || claude.PlanType != nil || claude.ActiveStart != nil || claude.ActiveUntil != nil {
		t.Fatalf("expected non-codex auth file to ignore id_token fields, got %+v", claude)
	}
	codexNoToken := byIdentity["codex-no-token"]
	if codexNoToken.AccountID != nil || codexNoToken.PlanType != nil || codexNoToken.ActiveStart != nil || codexNoToken.ActiveUntil != nil {
		t.Fatalf("expected codex auth file without id_token to keep nullable fields empty, got %+v", codexNoToken)
	}
}

func TestSyncMetadataWritesProjectIDFromAuthFileProjectID(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{{
			AuthIndex: "gemini-project",
			Type:      "gemini-cli",
			Provider:  "Gemini",
			ProjectID: "gemini-project-id",
		}, {
			AuthIndex: "antigravity-project",
			Type:      "antigravity",
			Provider:  "Antigravity",
			ProjectID: "antigravity-project-id",
		}, {
			AuthIndex: "claude-no-project",
			Type:      "claude",
			Provider:  "Claude",
			ProjectID: "ignored-project-id",
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
	if projectID := byIdentity["gemini-project"].ProjectID; projectID == nil || *projectID != "gemini-project-id" {
		t.Fatalf("expected gemini project_id to persist, got %+v", byIdentity["gemini-project"])
	}
	if projectID := byIdentity["antigravity-project"].ProjectID; projectID == nil || *projectID != "antigravity-project-id" {
		t.Fatalf("expected antigravity project_id to persist, got %+v", byIdentity["antigravity-project"])
	}
	if projectID := byIdentity["claude-no-project"].ProjectID; projectID != nil {
		t.Fatalf("expected unsupported auth file type to ignore project_id, got %+v", byIdentity["claude-no-project"])
	}
}

func TestSyncMetadataWritesProviderMetadataToUsageIdentities(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude Team", AuthIndex: "claude-auth-index", Priority: intPtr(2), Disabled: boolPtr(false), Note: strPtr("provider note")}},
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
	apiKey := byIdentity["claude-auth-index"]
	if apiKey.Name != "Claude Team" || apiKey.AuthType != entities.UsageIdentityAuthTypeAIProvider || apiKey.AuthTypeName != "apikey" || apiKey.Identity != "claude-auth-index" || apiKey.Type != "claude" || apiKey.LookupKey != "claude-key" || apiKey.Prefix != "claude-prefix" || apiKey.Provider != "Claude Team" || apiKey.IsDeleted {
		t.Fatalf("unexpected provider usage identity for api key: %+v", apiKey)
	}
	if apiKey.Priority == nil || *apiKey.Priority != 2 || apiKey.Disabled == nil || *apiKey.Disabled || apiKey.Note == nil || *apiKey.Note != "provider note" {
		t.Fatalf("expected provider sync metadata to persist, got %+v", apiKey)
	}
	if _, ok := byIdentity["claude-prefix"]; ok {
		t.Fatalf("expected provider prefix not to be stored as usage identity, got %+v", byIdentity["claude-prefix"])
	}
	assertTableNotExists(t, db, "provider_metadata")
}

func TestSyncMetadataStoresProviderBaseURLWithoutOverwritingNameOrProvider(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			CodexAPIKeys: []providerconfig.ProviderKeyConfig{
				{APIKey: "codex-key-a", BaseURL: "https://api.openai.com/v1", AuthIndex: "codex-auth-a"},
				{APIKey: "codex-key-b", Name: "Codex Team", BaseURL: "https://chatgpt.com/backend-api/codex/", AuthIndex: "codex-auth-b"},
			},
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
	unnamed := byIdentity["codex-auth-a"]
	if unnamed.Name != "codex" || unnamed.Provider != "codex" || unnamed.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected unnamed codex to keep provider identity and store base URL separately, got %+v", unnamed)
	}
	named := byIdentity["codex-auth-b"]
	if named.Name != "Codex Team" || named.Provider != "Codex Team" || named.BaseURL != "https://chatgpt.com/backend-api/codex/" {
		t.Fatalf("expected named codex to keep name/provider and store base URL separately, got %+v", named)
	}
}

func TestSyncMetadataStoresOpenAICompatibilityBaseURL(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			OpenAICompatibility: []providerconfig.OpenAICompatibilityConfig{
				{
					Name:     "OpenRouter",
					Prefix:   "openrouter",
					BaseURL:  "https://openrouter.ai/api/v1",
					Priority: intPtr(9),
					Disabled: boolPtr(true),
					Note:     strPtr("shared openai provider"),
					APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{
						{APIKey: "openrouter-key", AuthIndex: "openrouter-auth"},
						{APIKey: "openrouter-key-2", AuthIndex: "openrouter-auth-2"},
					},
				},
			},
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
	identity := byIdentity["openrouter-auth"]
	if identity.Name != "OpenRouter" || identity.Provider != "OpenRouter" || identity.Type != "openai" || identity.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("expected openai compatibility identity to keep name/provider/type and store base URL, got %+v", identity)
	}
	if identity.Priority == nil || *identity.Priority != 9 || identity.Disabled == nil || !*identity.Disabled || identity.Note == nil || *identity.Note != "shared openai provider" {
		t.Fatalf("expected openai compatibility provider-level sync metadata to persist, got %+v", identity)
	}
	second := byIdentity["openrouter-auth-2"]
	if second.Priority == nil || *second.Priority != 9 || second.Disabled == nil || !*second.Disabled || second.Note == nil || *second.Note != "shared openai provider" {
		t.Fatalf("expected openai compatibility metadata to apply to every entry, got %+v", second)
	}
}

func TestSyncMetadataKeepsProviderIdentityWhenPrefixEqualsAPIKey(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "same-value", Prefix: "same-value", Name: "Claude Same", AuthIndex: "same-auth-index"}},
		}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	var identity entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", entities.UsageIdentityAuthTypeAIProvider, "same-auth-index").First(&identity).Error; err != nil {
		t.Fatalf("load protected api key usage identity: %v", err)
	}
	if identity.IsDeleted || identity.Type != "claude" || identity.Provider != "Claude Same" || identity.LookupKey != "same-value" {
		t.Fatalf("expected api key matching prefix to remain active, got %+v", identity)
	}
}

func TestSyncMetadataDoesNotUseOpenAICompatibilityPrefixAsDisplayName(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			OpenAICompatibility: []providerconfig.OpenAICompatibilityConfig{{
				Prefix:        "https://proxy.internal/v1",
				APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-compatible-key", AuthIndex: "openai-compatible-auth-index"}},
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
	identity := byIdentity["openai-compatible-auth-index"]
	if identity.Identity != "openai-compatible-auth-index" || identity.LookupKey != "openai-compatible-key" {
		t.Fatalf("expected OpenAI compatibility api key usage identity, got %+v", identity)
	}
	if identity.Name != "openai" || identity.Provider != "openai" || identity.Prefix != "https://proxy.internal/v1" {
		t.Fatalf("expected raw OpenAI compatibility prefix to be stored only as prefix metadata, got %+v", identity)
	}
	if _, ok := byIdentity["https://proxy.internal/v1"]; ok {
		t.Fatalf("expected OpenAI compatibility prefix not to create usage identity, got %+v", items)
	}
}

func TestSyncMetadataUsageIdentityPartialFailureKeepsFailedProviderType(t *testing.T) {
	db := openSyncTestDatabase(t)
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	oldDeletedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&[]entities.UsageIdentity{{
		Name:         "Old Gemini",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-gemini-key",
		Type:         "gemini",
		Provider:     "Old Gemini",
	}, {
		Name:         "Old Claude",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
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
			providerConfig: providerconfig.ProviderMetadataConfig{ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "new-claude-key", Prefix: "new-claude-prefix", Name: "New Claude", AuthIndex: "new-claude-auth-index"}}},
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
	if newClaude := byIdentity["new-claude-auth-index"]; newClaude.Identity == "" || newClaude.LookupKey != "new-claude-key" || newClaude.IsDeleted {
		t.Fatalf("expected new claude identity to be active, got %+v", newClaude)
	}
}

func TestSyncMetadataAggregatesUsageIdentityStatsAfterUpsert(t *testing.T) {
	db := openSyncTestDatabase(t)
	eventTime := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
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
		MetadataFetcher: stubMetadataFetcher{authFilesResult: &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{{
			AuthIndex: "auth-stat",
			Email:     "stats@example.com",
			Type:      "claude",
			Provider:  "Claude",
		}}}}},
	})

	if err := service.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	var identity entities.UsageIdentity
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
		MetadataFetcher: stubMetadataFetcher{providerConfig: providerconfig.ProviderMetadataConfig{
			GeminiAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "gemini-key", Prefix: "gemini-prefix", Name: "Gemini", AuthIndex: "gemini-auth-index"}},
			ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude", AuthIndex: "claude-auth-index"}},
			OpenAICompatibility: []providerconfig.OpenAICompatibilityConfig{{
				Name:          "Custom OpenAI",
				Prefix:        "custom-openai",
				APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "custom-key", AuthIndex: "custom-auth-index"}},
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
	expectedMetadata := map[string]struct {
		lookupKey string
		prefix    string
	}{
		"gemini-auth-index": {lookupKey: "gemini-key", prefix: "gemini-prefix"},
		"claude-auth-index": {lookupKey: "claude-key", prefix: "claude-prefix"},
		"custom-auth-index": {lookupKey: "custom-key", prefix: "custom-openai"},
	}
	for expected, metadata := range expectedMetadata {
		identity := providerItems[expected]
		if identity.Identity != expected || identity.LookupKey != metadata.lookupKey || identity.Prefix != metadata.prefix || identity.AuthType != entities.UsageIdentityAuthTypeAIProvider || identity.AuthTypeName != "apikey" || identity.IsDeleted {
			t.Fatalf("expected active provider usage identity %q, got %+v", expected, identity)
		}
	}
	for _, prefix := range []string{"gemini-prefix", "claude-prefix", "custom-openai"} {
		if _, ok := providerItems[prefix]; ok {
			t.Fatalf("expected provider prefix %q not to create usage identity, got %+v", prefix, items)
		}
	}
	assertTableNotExists(t, db, "provider_metadata")
}

func TestSyncMetadataPersistsSuccessfulProviderUsageIdentitiesWhenOneEndpointFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubMetadataFetcher{
			providerConfig: providerconfig.ProviderMetadataConfig{ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude", AuthIndex: "claude-auth-index"}}},
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
	identity := byIdentity["claude-auth-index"]
	if identity.Identity != "claude-auth-index" || identity.LookupKey != "claude-key" || identity.Type != "claude" || identity.AuthType != entities.UsageIdentityAuthTypeAIProvider || identity.IsDeleted {
		t.Fatalf("expected successful provider usage identity to persist, got %+v", identity)
	}
	if _, ok := byIdentity["claude-prefix"]; ok {
		t.Fatalf("expected successful provider prefix not to create usage identity, got %+v", items)
	}
	if _, ok := byIdentity["gemini-key"]; ok {
		t.Fatalf("expected failed gemini endpoint not to create usage identity, got %+v", items)
	}
}

func TestSyncMetadataKeepsFailedProviderUsageIdentitiesDuringPartialFailure(t *testing.T) {
	db := openSyncTestDatabase(t)
	now := time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)
	if err := db.Create(&[]entities.UsageIdentity{{
		Name:         "Old Gemini",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "old-gemini-key",
		Type:         "gemini",
		Provider:     "Old Gemini",
	}, {
		Name:         "Old Claude",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
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
			providerConfig: providerconfig.ProviderMetadataConfig{ClaudeAPIKeys: []providerconfig.ProviderKeyConfig{{APIKey: "new-claude-key", Prefix: "new-claude-prefix", Name: "New Claude", AuthIndex: "new-claude-auth-index"}}},
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
	newClaude := byIdentity["new-claude-auth-index"]
	if newClaude.Identity != "new-claude-auth-index" || newClaude.LookupKey != "new-claude-key" || newClaude.IsDeleted {
		t.Fatalf("expected active replacement usage identity, got %+v", newClaude)
	}
	if _, ok := byIdentity["new-claude-prefix"]; ok {
		t.Fatalf("expected replacement prefix not to create usage identity, got %+v", items)
	}
}

func TestSyncMetadataKeepsProviderUsageIdentitiesWhenEndpointReturnsNilResult(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Old Gemini",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
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

func TestSplitRedisUsageSyncErrorDoesNotCreateSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("dial failed")},
	})

	result, err := processPendingOrPulledRedisUsageForTest(t, service)
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

func equivalentRedisMessage(apiGroupKey, model string, timestamp time.Time, source, authIndex string, failed bool, latencyMS int64, tokens dto.TokenStats, requestID string) string {
	failedValue := "false"
	if failed {
		failedValue = "true"
	}
	return `{"timestamp":"` + timestamp.UTC().Format(time.RFC3339) + `","latency_ms":` + int64String(latencyMS) + `,"source":"` + source + `","auth_index":"` + authIndex + `","failed":` + failedValue + `,"api_key":"` + apiGroupKey + `","model":"` + model + `","request_id":"` + requestID + `","tokens":{"input_tokens":` + int64String(tokens.InputTokens) + `,"output_tokens":` + int64String(tokens.OutputTokens) + `,"reasoning_tokens":` + int64String(tokens.ReasoningTokens) + `,"cached_tokens":` + int64String(tokens.CachedTokens) + `,"total_tokens":` + int64String(tokens.TotalTokens) + `}}`
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

func strPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func usageIdentitiesByIdentity(items []entities.UsageIdentity) map[string]entities.UsageIdentity {
	byIdentity := make(map[string]entities.UsageIdentity, len(items))
	for _, item := range items {
		byIdentity[item.Identity] = item
	}
	return byIdentity
}

func assertUsageEventCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
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
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}
	return db, logs
}
