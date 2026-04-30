package service

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
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

type stubExportFetcher struct {
	result          *cpa.ExportResult
	err             error
	authFilesResult *cpa.AuthFilesResult
	authFilesErr    error
	providerConfig  cpa.ManagementConfig
	geminiErr       error
	claudeErr       error
	codexErr        error
	vertexErr       error
	openAIErr       error
}

type stubBackupWriter struct {
	path    string
	payload []byte
	err     error
	calls   int
}

type stubBackupCleaner struct {
	retentionDays int
	now           time.Time
	err           error
	calls         int
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

func (s stubExportFetcher) FetchUsageExport(context.Context) (*cpa.ExportResult, error) {
	return s.result, s.err
}

func (s stubExportFetcher) FetchAuthFiles(context.Context) (*cpa.AuthFilesResult, error) {
	if s.authFilesResult != nil || s.authFilesErr != nil {
		return s.authFilesResult, s.authFilesErr
	}
	return &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{}}, nil
}

func (s stubExportFetcher) FetchGeminiAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.GeminiAPIKeys, s.geminiErr)
}

func (s stubExportFetcher) FetchClaudeAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.ClaudeAPIKeys, s.claudeErr)
}

func (s stubExportFetcher) FetchCodexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.CodexAPIKeys, s.codexErr)
}

func (s stubExportFetcher) FetchVertexAPIKeys(context.Context) (*cpa.ProviderKeyConfigResult, error) {
	return providerKeyConfigResult(s.providerConfig.VertexAPIKeys, s.vertexErr)
}

func (s stubExportFetcher) FetchOpenAICompatibility(context.Context) (*cpa.OpenAICompatibilityResult, error) {
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

func (s *stubBackupWriter) Write(_ uint, _ time.Time, payload []byte) (string, error) {
	s.calls++
	s.payload = append([]byte(nil), payload...)
	if s.err != nil {
		return "", s.err
	}
	return s.path, nil
}

func (s *stubBackupCleaner) Cleanup(retentionDays int, now time.Time) (int, error) {
	s.calls++
	s.retentionDays = retentionDays
	s.now = now
	return 0, s.err
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

func (s *trackingMetadataFetcher) providerCalls() int {
	return s.geminiCalls + s.claudeCalls + s.codexCalls + s.vertexCalls + s.openAICalls
}

func TestSyncOncePersistsSnapshotAndEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	body := []byte(`{"version":1,"exported_at":"2026-04-16T10:00:00Z","usage":{"apis":{"provider-a":{"models":{"claude-sonnet":{"details":[{"timestamp":"2026-04-16T09:30:00Z","latency_ms":123,"source":"codex-a","auth_index":"1","failed":false,"tokens":{"input_tokens":10,"output_tokens":20,"reasoning_tokens":5,"cached_tokens":0,"total_tokens":35}}]}}}}}}`)
	backupWriter := &stubBackupWriter{path: "/tmp/export.json"}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		Client: stubExportFetcher{
			result: successfulExportResult(body),
			authFilesResult: &cpa.AuthFilesResult{StatusCode: 200, Payload: cpa.AuthFilesResponse{Files: []cpa.AuthFile{{
				AuthIndex: "1",
				Name:      "Claude Desktop",
				Email:     "user@example.com",
				Type:      "claude",
				Provider:  "anthropic",
			}}}},
		},
		BackupEnabled: true,
		BackupWriter:  backupWriter,
	})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncOnce returned error: %v", err)
	}
	if result.Status != "completed" || result.HTTPStatus != 200 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("unexpected sync counts: %+v", result)
	}
	if result.BackupFilePath != "/tmp/export.json" || backupWriter.calls != 1 {
		t.Fatalf("expected backup file path to be recorded, got result=%+v calls=%d", result, backupWriter.calls)
	}
	if string(backupWriter.payload) != string(body) {
		t.Fatalf("expected backup payload to match raw body, got %s", string(backupWriter.payload))
	}

	var snapshot models.SnapshotRun
	if err := db.First(&snapshot, result.SnapshotRunID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed snapshot run, got %q", snapshot.Status)
	}
	if snapshot.PayloadHash == "" || snapshot.InsertedEvents != 1 {
		t.Fatalf("unexpected snapshot values: %+v", snapshot)
	}
	if snapshot.BackupFilePath != "/tmp/export.json" {
		t.Fatalf("expected snapshot backup path to be stored, got %q", snapshot.BackupFilePath)
	}

	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.SnapshotRunID != result.SnapshotRunID || event.Source != "codex-a" || event.TotalTokens != 35 {
		t.Fatalf("unexpected usage event: %+v", event)
	}

	var authFile models.AuthFile
	if err := db.First(&authFile).Error; err != nil {
		t.Fatalf("load auth file: %v", err)
	}
	if authFile.AuthIndex != "1" || authFile.Email != "user@example.com" {
		t.Fatalf("unexpected auth file: %+v", authFile)
	}
}

func TestSyncOnceMarksFetchFailureOnSnapshotRun(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{
		err: errors.New("management export request failed with status 401"),
		result: &cpa.ExportResult{
			StatusCode: 401,
			Body:       []byte(`{"error":"unauthorized"}`),
		},
	})

	result, err := service.SyncNow(context.Background())
	if err == nil {
		t.Fatal("expected sync error")
	}
	if result == nil || result.Status != "failed" || result.HTTPStatus != 401 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	var snapshot models.SnapshotRun
	if err := db.First(&snapshot, result.SnapshotRunID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if snapshot.Status != "failed" {
		t.Fatalf("expected failed snapshot run, got %q", snapshot.Status)
	}
	if snapshot.ErrorMessage == "" {
		t.Fatal("expected snapshot error message to be stored")
	}
}

func TestSyncOnceReturnsAuthFilesFailureWithoutClearingExistingData(t *testing.T) {
	db := openSyncTestDatabase(t)
	if err := repository.ReplaceAuthFiles(db, []repository.AuthFileInput{{
		AuthIndex: "existing",
		Email:     "existing@example.com",
	}}); err != nil {
		t.Fatalf("seed auth files: %v", err)
	}

	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{
		result:       successfulExportResult([]byte(`{"version":1}`)),
		authFilesErr: errors.New("management auth files request failed with status 503"),
	})

	result, err := service.SyncNow(context.Background())
	if err == nil {
		t.Fatal("expected auth files sync error")
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected completed_with_warnings sync result with partial failure, got %+v", result)
	}

	files, listErr := repository.ListAuthFiles(db)
	if listErr != nil {
		t.Fatalf("list auth files: %v", listErr)
	}
	if len(files) != 1 || files[0].AuthIndex != "existing" {
		t.Fatalf("expected existing auth files to remain available, got %+v", files)
	}

	var snapshot models.SnapshotRun
	if err := db.First(&snapshot, result.SnapshotRunID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if snapshot.Status != "completed_with_warnings" || snapshot.ErrorMessage == "" {
		t.Fatalf("expected completed_with_warnings snapshot with error message, got %+v", snapshot)
	}
}

func TestSyncOnceDeduplicatesExistingEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))})

	first, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("first SyncOnce returned error: %v", err)
	}
	second, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("second SyncOnce returned error: %v", err)
	}
	if first.InsertedEvents != 1 || second.InsertedEvents != 0 || second.DedupedEvents != 1 {
		t.Fatalf("unexpected dedup results: first=%+v second=%+v", first, second)
	}
}

func TestSyncOnceSkipsBackupWhenDisabled(t *testing.T) {
	db, logs := openSyncTestDatabaseWithLogs(t)
	backupWriter := &stubBackupWriter{path: "/tmp/export.json"}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:       "https://cpa.example.com",
		Client:        stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))},
		BackupEnabled: false,
		BackupWriter:  backupWriter,
	})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncOnce returned error: %v", err)
	}
	if result.BackupFilePath != "" {
		t.Fatalf("expected empty backup path, got %+v", result)
	}
	if backupWriter.calls != 0 {
		t.Fatalf("expected backup writer not to be called, got %d", backupWriter.calls)
	}
	if strings.Contains(logs.String(), "/internal/repository/db.go:156 record not found") {
		t.Fatalf("expected no backup snapshot lookup log when backup is disabled, got %s", logs.String())
	}
}

func TestSyncOnceFiltersEventsOlderThanLocalWatermarkOverlap(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedTime := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{{
		EventKey:      "seed-event",
		SnapshotRunID: 1,
		APIGroupKey:   "provider-a",
		Model:         "claude-sonnet",
		Timestamp:     seedTime,
		Source:        "seed-source",
		AuthIndex:     "1",
		TotalTokens:   10,
	}}); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{result: &cpa.ExportResult{
		StatusCode: 200,
		Payload: cpa.UsageExport{
			Version:    1,
			ExportedAt: seedTime.Add(time.Hour),
			Usage: cpa.StatisticsSnapshot{APIs: map[string]cpa.APISnapshot{
				"provider-a": {Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {Details: []cpa.RequestDetail{
						{Timestamp: seedTime.Add(-48 * time.Hour), Source: "old-source", AuthIndex: "2", Tokens: cpa.TokenStats{InputTokens: 1, OutputTokens: 1}},
						{Timestamp: seedTime.Add(-12 * time.Hour), Source: "recent-source", AuthIndex: "3", Tokens: cpa.TokenStats{InputTokens: 2, OutputTokens: 2}},
					}},
				}},
			}},
		},
	}})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("expected only recent event to be inserted, got %+v", result)
	}

	var count int64
	if err := db.Model(&models.UsageEvent{}).Where("source = ?", "old-source").Count(&count).Error; err != nil {
		t.Fatalf("count old filtered events: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected old event to be filtered out, found %d rows", count)
	}
	if err := db.Model(&models.UsageEvent{}).Where("source = ?", "recent-source").Count(&count).Error; err != nil {
		t.Fatalf("count recent events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected recent event to be inserted, found %d rows", count)
	}
}

func TestSyncOnceKeepsOverlapWindowEventsForExistingDedupe(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedTime := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	seedTokens := cpa.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 35}
	seedEvent := models.UsageEvent{
		EventKey:        BuildEventKey("provider-a", "claude-sonnet", seedTime.Add(-2*time.Hour), "codex-a", "1", false, seedTokens),
		SnapshotRunID:   1,
		APIGroupKey:     "provider-a",
		Model:           "claude-sonnet",
		Timestamp:       seedTime.Add(-2 * time.Hour),
		Source:          "codex-a",
		AuthIndex:       "1",
		TotalTokens:     35,
		InputTokens:     10,
		OutputTokens:    20,
		ReasoningTokens: 5,
	}
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{seedEvent}); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{result: &cpa.ExportResult{
		StatusCode: 200,
		Payload: cpa.UsageExport{
			Version:    1,
			ExportedAt: seedTime.Add(time.Hour),
			Usage: cpa.StatisticsSnapshot{APIs: map[string]cpa.APISnapshot{
				"provider-a": {Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {Details: []cpa.RequestDetail{{
						Timestamp: seedTime.Add(-2 * time.Hour),
						Source:    "codex-a",
						AuthIndex: "1",
						Tokens:    seedTokens,
					}}},
				}},
			}},
		},
	}})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if result.InsertedEvents != 0 || result.DedupedEvents != 1 {
		t.Fatalf("expected overlap event to reach dedupe path, got %+v", result)
	}
}

func TestSyncOnceKeepsZeroTimestampEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	seedTime := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	if _, _, err := repository.InsertUsageEvents(db, []models.UsageEvent{{
		EventKey:      "seed-event",
		SnapshotRunID: 1,
		APIGroupKey:   "provider-a",
		Model:         "claude-sonnet",
		Timestamp:     seedTime,
		Source:        "seed-source",
		AuthIndex:     "1",
		TotalTokens:   10,
	}}); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	service := NewSyncServiceWithClient(db, "https://cpa.example.com", stubExportFetcher{result: &cpa.ExportResult{
		StatusCode: 200,
		Payload: cpa.UsageExport{
			Version: 1,
			Usage: cpa.StatisticsSnapshot{APIs: map[string]cpa.APISnapshot{
				"provider-a": {Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {Details: []cpa.RequestDetail{{
						Source:    "zero-ts-source",
						AuthIndex: "5",
						Tokens:    cpa.TokenStats{InputTokens: 3, OutputTokens: 4},
					}}},
				}},
			}},
		},
	}})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if result.InsertedEvents != 1 {
		t.Fatalf("expected zero timestamp event to be kept, got %+v", result)
	}
}

func TestSyncOnceSkipsBackupWithinConfiguredInterval(t *testing.T) {
	db := openSyncTestDatabase(t)
	body := []byte(`{"version":1,"exported_at":"2026-04-16T10:00:00Z","usage":{"apis":{"provider-a":{"models":{"claude-sonnet":{"details":[{"timestamp":"2026-04-16T09:30:00Z","latency_ms":123,"source":"codex-a","auth_index":"1","failed":false,"tokens":{"input_tokens":10,"output_tokens":20,"reasoning_tokens":5,"cached_tokens":0,"total_tokens":35}}]}}}}}}`)
	backupWriter := &stubBackupWriter{path: "/tmp/export.json"}
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:        "https://cpa.example.com",
		Client:         stubExportFetcher{result: successfulExportResult(body)},
		BackupEnabled:  true,
		BackupWriter:   backupWriter,
		BackupInterval: time.Hour,
		Now: func() time.Time {
			return now
		},
	})

	first, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("first SyncNow returned error: %v", err)
	}
	if first.BackupFilePath != "/tmp/export.json" {
		t.Fatalf("expected first sync to write backup, got %+v", first)
	}

	now = now.Add(30 * time.Minute)
	second, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("second SyncNow returned error: %v", err)
	}

	if second.BackupFilePath != "" {
		t.Fatalf("expected second sync to skip backup, got %+v", second)
	}
	if backupWriter.calls != 1 {
		t.Fatalf("expected backup writer to be called once, got %d", backupWriter.calls)
	}

	var snapshots []models.SnapshotRun
	if err := db.Order("id ASC").Find(&snapshots).Error; err != nil {
		t.Fatalf("load snapshot runs: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshot runs, got %d", len(snapshots))
	}
	if snapshots[0].BackupFilePath == "" {
		t.Fatalf("expected first snapshot backup path to be recorded, got %+v", snapshots[0])
	}
	if snapshots[1].Status != "completed" || snapshots[1].BackupFilePath != "" {
		t.Fatalf("expected second snapshot to complete without backup path, got %+v", snapshots[1])
	}
}

func TestSyncOnceWritesBackupAgainAfterConfiguredInterval(t *testing.T) {
	db := openSyncTestDatabase(t)
	backupWriter := &stubBackupWriter{path: "/tmp/export.json"}
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:        "https://cpa.example.com",
		BackupEnabled:  true,
		BackupWriter:   backupWriter,
		BackupInterval: time.Hour,
		Now: func() time.Time {
			return now
		},
	})

	service.client = stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))}
	first, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("first SyncNow returned error: %v", err)
	}

	now = now.Add(time.Hour)
	service.client = stubExportFetcher{result: successfulExportResult([]byte(`{"version":2}`))}
	second, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("second SyncNow returned error: %v", err)
	}

	if first.BackupFilePath == "" || second.BackupFilePath == "" {
		t.Fatalf("expected both syncs to write backups, got first=%+v second=%+v", first, second)
	}
	if backupWriter.calls != 2 {
		t.Fatalf("expected backup writer to be called twice, got %d", backupWriter.calls)
	}
}

func TestSyncOnceFailsWhenBackupWriteFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:       "https://cpa.example.com",
		Client:        stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))},
		BackupEnabled: true,
		BackupWriter:  &stubBackupWriter{err: errors.New("disk full")},
	})

	_, err := service.SyncNow(context.Background())
	if err == nil || err.Error() != "write backup: disk full" {
		t.Fatalf("expected backup write error, got %v", err)
	}

	var snapshot models.SnapshotRun
	if err := db.Last(&snapshot).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if snapshot.Status != "failed" || snapshot.ErrorMessage != "disk full" {
		t.Fatalf("unexpected snapshot after backup failure: %+v", snapshot)
	}
}

func TestSyncOnceCleansBackupsAfterSuccessfulSync(t *testing.T) {
	db := openSyncTestDatabase(t)
	backupWriter := &stubBackupWriter{path: "/tmp/export.json"}
	backupCleaner := &stubBackupCleaner{}
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:             "https://cpa.example.com",
		Client:              stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))},
		BackupEnabled:       true,
		BackupWriter:        backupWriter,
		BackupRetentionDays: 3,
		BackupCleaner:       backupCleaner,
		Now: func() time.Time {
			return now
		},
	})

	_, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if backupCleaner.calls != 1 {
		t.Fatalf("expected backup cleaner to be called once, got %d", backupCleaner.calls)
	}
	if backupCleaner.retentionDays != 3 || !backupCleaner.now.Equal(now) {
		t.Fatalf("unexpected cleanup input: %+v", backupCleaner)
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

	result, err := service.SyncRedisBatch(context.Background(), true)
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || !result.Empty || result.Status != "empty" {
		t.Fatalf("expected empty redis batch result, got %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped for empty batch, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

	var snapshotCount int64
	if err := db.Model(&models.SnapshotRun{}).Count(&snapshotCount).Error; err != nil {
		t.Fatalf("count snapshot runs: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected no snapshot runs for empty batch, got %d", snapshotCount)
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

	result, err := service.SyncRedisBatch(context.Background(), false)
	if err != nil {
		t.Fatalf("SyncRedisBatch returned error: %v", err)
	}
	if result == nil || result.Empty || result.Status != "completed" || result.InsertedEvents != 1 || result.DedupedEvents != 0 {
		t.Fatalf("unexpected redis batch result: %+v", result)
	}
	if metadata.authCalls != 0 || metadata.providerCalls() != 0 {
		t.Fatalf("expected metadata fetch to be skipped, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
	}

	var snapshot models.SnapshotRun
	if err := db.First(&snapshot, result.SnapshotRunID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if snapshot.Status != "completed" || snapshot.InsertedEvents != 1 {
		t.Fatalf("unexpected snapshot run: %+v", snapshot)
	}
	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-1" || event.SnapshotRunID != result.SnapshotRunID {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	var inbox models.RedisUsageInbox
	if err := db.First(&inbox).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.SnapshotRunID == nil || *inbox.SnapshotRunID != result.SnapshotRunID || inbox.UsageEventKey != "redis-1" {
		t.Fatalf("expected processed inbox row linked to snapshot, got %+v", inbox)
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

	result, err := service.SyncRedisBatch(context.Background(), false)
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

	result, err := service.SyncRedisBatch(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode warning, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" {
		t.Fatalf("expected warning result, got %+v", result)
	}

	var snapshotCount int64
	if err := db.Model(&models.SnapshotRun{}).Count(&snapshotCount).Error; err != nil {
		t.Fatalf("count snapshot runs: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected no snapshot for malformed-only batch, got %d", snapshotCount)
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
		QueueKey:   "queue",
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

	result, err := service.SyncRedisBatch(context.Background(), false)
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
		EventKey:      "future-watermark",
		SnapshotRunID: 1,
		APIGroupKey:   "claude",
		Model:         "sonnet",
		Timestamp:     time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed future event: %v", err)
	}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		RedisQueue: staticRedisQueue{messages: []string{
			`{"timestamp":"2026-04-26T07:00:00Z","provider":"claude","model":"sonnet","request_id":"old-but-unique","tokens":{"input_tokens":1,"output_tokens":2}}`,
		}},
	})

	result, err := service.SyncRedisBatch(context.Background(), false)
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
		QueueKey:   "queue",
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

	result, err := service.SyncRedisBatch(context.Background(), false)
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

func TestSyncNowInRedisBackedModesUsesDurableInbox(t *testing.T) {
	for _, mode := range []string{"redis", "auto"} {
		t.Run(mode, func(t *testing.T) {
			db := openSyncTestDatabase(t)
			service := NewSyncServiceWithOptions(db, SyncServiceOptions{
				BaseURL:       "https://cpa.example.com",
				UsageSyncMode: mode,
				RedisQueue: staticRedisQueue{messages: []string{
					`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"sync-now-` + mode + `","tokens":{"input_tokens":1,"output_tokens":2}}`,
				}},
				MetadataFetcher: stubExportFetcher{},
			})

			result, err := service.SyncNow(context.Background())
			if err != nil {
				t.Fatalf("SyncNow returned error: %v", err)
			}
			if result == nil || result.InsertedEvents != 1 {
				t.Fatalf("unexpected SyncNow result: %+v", result)
			}
			var inbox models.RedisUsageInbox
			if err := db.First(&inbox).Error; err != nil {
				t.Fatalf("load inbox row: %v", err)
			}
			if inbox.Status != repository.RedisUsageInboxStatusProcessed || inbox.UsageEventKey != "sync-now-"+mode {
				t.Fatalf("expected SyncNow %s path to use inbox, got %+v", mode, inbox)
			}
		})
	}
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

	_, err := service.SyncRedisBatch(context.Background(), false)
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

func TestSyncOnceWritesCoreDebugLogsForLegacyPull(t *testing.T) {
	db := openSyncTestDatabase(t)
	logs := captureSyncDebugLogs(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		Client:  stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))},
	})

	_, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	output := logs.String()
	for _, expected := range []string{
		"legacy usage pull started",
		"legacy usage pull finished",
		"usage persistence started",
		"usage events insert finished",
		"snapshot run finalized",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected debug log %q in output:\n%s", expected, output)
		}
	}
}

func TestAutoUsageFetcherWritesDebugLogsForFallback(t *testing.T) {
	logs := captureSyncDebugLogs(t)
	fetcher := autoUsageFetcher{
		primary:  redisUsageFetcher{queue: staticRedisQueue{err: errors.New("dial failed")}},
		fallback: legacyUsageFetcher{client: stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))}},
	}

	_, err := fetcher.FetchUsage(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	output := logs.String()
	for _, expected := range []string{
		"auto usage redis pull started",
		"auto usage redis pull failed, fallback started",
		"legacy usage pull started",
		"legacy usage pull finished",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected debug log %q in output:\n%s", expected, output)
		}
	}
}

func TestSyncRedisBatchReturnsMetadataWarningAfterPersistingEvents(t *testing.T) {
	db := openSyncTestDatabase(t)
	metadata := &trackingMetadataFetcher{authErr: errors.New("metadata unavailable")}
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		RedisQueue:      staticRedisQueue{messages: []string{`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-2","tokens":{"input_tokens":1,"output_tokens":2}}`}},
		MetadataFetcher: metadata,
	})

	result, err := service.SyncRedisBatch(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "metadata unavailable") {
		t.Fatalf("expected metadata warning error, got %v", err)
	}
	if result == nil || result.Status != "completed_with_warnings" || result.InsertedEvents != 1 {
		t.Fatalf("expected warning result with persisted event, got %+v", result)
	}
	if metadata.authCalls != 1 || metadata.providerCalls() != 5 {
		t.Fatalf("expected metadata fetch once, got auth=%d provider=%d", metadata.authCalls, metadata.providerCalls())
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
	var snapshotCount int64
	if err := db.Model(&models.SnapshotRun{}).Count(&snapshotCount).Error; err != nil {
		t.Fatalf("count snapshot runs: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected metadata sync not to create snapshots, got %d", snapshotCount)
	}
}

func TestSyncMetadataPersistsProviderMetadataFromDedicatedEndpoints(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubExportFetcher{providerConfig: cpa.ManagementConfig{
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
	items, err := repository.ListProviderMetadata(db)
	if err != nil {
		t.Fatalf("list provider metadata: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("expected provider metadata rows from dedicated endpoints, got %+v", items)
	}
}

func TestSyncMetadataPersistsSuccessfulProviderMetadataWhenOneEndpointFails(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: "https://cpa.example.com",
		MetadataFetcher: stubExportFetcher{
			providerConfig: cpa.ManagementConfig{ClaudeAPIKeys: []cpa.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude"}}},
			geminiErr:      errors.New("gemini unavailable"),
		},
	})

	err := service.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gemini unavailable") {
		t.Fatalf("expected provider metadata warning, got %v", err)
	}
	items, listErr := repository.ListProviderMetadata(db)
	if listErr != nil {
		t.Fatalf("list provider metadata: %v", listErr)
	}
	if len(items) != 2 || items[0].ProviderType != "claude" {
		t.Fatalf("expected successful provider metadata to persist, got %+v", items)
	}
}

func TestSyncRedisBatchErrorDoesNotCreateSnapshot(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:    "https://cpa.example.com",
		RedisQueue: staticRedisQueue{err: errors.New("dial failed")},
	})

	result, err := service.SyncRedisBatch(context.Background(), false)
	if err == nil || result == nil || result.Status != "failed" {
		t.Fatalf("expected failed redis batch result, got result=%+v err=%v", result, err)
	}
	var snapshotCount int64
	if countErr := db.Model(&models.SnapshotRun{}).Count(&snapshotCount).Error; countErr != nil {
		t.Fatalf("count snapshot runs: %v", countErr)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected no snapshot runs after redis pop error, got %d", snapshotCount)
	}
}

func TestSyncOnceUsesRedisUsageFetcher(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		UsageFetcher:    redisUsageFetcher{queue: staticRedisQueue{messages: []string{`{"timestamp":"2026-04-27T08:00:00Z","provider":"claude","model":"sonnet","request_id":"redis-1","tokens":{"input_tokens":1,"output_tokens":2}}`}}},
		MetadataFetcher: stubExportFetcher{},
	})

	result, err := service.SyncNow(context.Background())
	if err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if result.InsertedEvents != 1 || result.HTTPStatus != 0 {
		t.Fatalf("unexpected redis sync result: %+v", result)
	}

	var event models.UsageEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.EventKey != "redis-1" || event.APIGroupKey != "claude" || event.Model != "sonnet" {
		t.Fatalf("unexpected redis usage event: %+v", event)
	}
}

func TestAutoUsageFetcherFallsBackOnRedisFailure(t *testing.T) {
	fallbackBody := []byte(`{"version":1}`)
	fetcher := autoUsageFetcher{
		primary:  redisUsageFetcher{queue: staticRedisQueue{err: errors.New("dial failed")}},
		fallback: legacyUsageFetcher{client: stubExportFetcher{result: successfulExportResult(fallbackBody)}},
	}

	result, err := fetcher.FetchUsage(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if result.HTTPStatus != 200 || string(result.RawPayload) != string(fallbackBody) || len(result.Events) != 1 {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
}

func TestAutoUsageFetcherDoesNotFallbackOnRedisAuthFailure(t *testing.T) {
	fetcher := autoUsageFetcher{
		primary:  redisUsageFetcher{queue: staticRedisQueue{err: cpa.ErrRedisQueueAuth}},
		fallback: legacyUsageFetcher{client: stubExportFetcher{result: successfulExportResult([]byte(`{"version":1}`))}},
	}

	_, err := fetcher.FetchUsage(context.Background(), time.Now())
	if !errors.Is(err, cpa.ErrRedisQueueAuth) {
		t.Fatalf("expected redis auth error, got %v", err)
	}
}

func TestNewSyncServiceBuildsClientFromConfig(t *testing.T) {
	db := openSyncTestDatabase(t)
	service := NewSyncService(db, config.Config{
		CPABaseURL:       " https://cpa.example.com ",
		CPAManagementKey: "secret",
		RequestTimeout:   5 * time.Second,
		BackupEnabled:    true,
		BackupDir:        "/tmp/backups",
		BackupInterval:   2 * time.Hour,
	})
	if service == nil || service.client == nil {
		t.Fatal("expected sync service client to be initialized")
	}
	if service.baseURL != "https://cpa.example.com" {
		t.Fatalf("expected trimmed base url, got %q", service.baseURL)
	}
	if service.backupWriter == nil {
		t.Fatal("expected backup writer to be initialized when backups are enabled")
	}
	if service.backupInterval != 2*time.Hour {
		t.Fatalf("expected backup interval to be initialized, got %s", service.backupInterval)
	}
}

func successfulExportResult(body []byte) *cpa.ExportResult {
	return &cpa.ExportResult{
		StatusCode: 200,
		Body:       body,
		Payload: cpa.UsageExport{
			Version:    1,
			ExportedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Usage: cpa.StatisticsSnapshot{
				APIs: map[string]cpa.APISnapshot{
					"provider-a": {
						Models: map[string]cpa.ModelSnapshot{
							"claude-sonnet": {
								Details: []cpa.RequestDetail{{
									Timestamp: time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC),
									LatencyMS: 123,
									Source:    "codex-a",
									AuthIndex: "1",
									Tokens:    cpa.TokenStats{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 35},
								}},
							},
						},
					},
				},
			},
		},
	}
}

func openSyncTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "sync.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	return db
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
	if err := db.AutoMigrate(models.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}
	return db, logs
}
