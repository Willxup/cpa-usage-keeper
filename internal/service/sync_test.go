package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type stubExportFetcher struct {
	result                 *cpa.ExportResult
	err                    error
	authFilesResult        *cpa.AuthFilesResult
	authFilesErr           error
	managementConfigResult *cpa.ManagementConfigResult
	managementConfigErr    error
}

type stubBackupWriter struct {
	path    string
	payload []byte
	err     error
	calls   int
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

func (s stubExportFetcher) FetchManagementConfig(context.Context) (*cpa.ManagementConfigResult, error) {
	if s.managementConfigResult != nil || s.managementConfigErr != nil {
		return s.managementConfigResult, s.managementConfigErr
	}
	return &cpa.ManagementConfigResult{StatusCode: 200, Payload: cpa.ManagementConfig{}}, nil
}

func (s *stubBackupWriter) Write(_ uint, _ time.Time, payload []byte) (string, error) {
	s.calls++
	s.payload = append([]byte(nil), payload...)
	if s.err != nil {
		return "", s.err
	}
	return s.path, nil
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
	if result == nil || result.Status != "completed" {
		t.Fatalf("expected completed sync result with partial failure, got %+v", result)
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
	if snapshot.Status != "completed" || snapshot.ErrorMessage == "" {
		t.Fatalf("expected completed snapshot with error message, got %+v", snapshot)
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
	db := openSyncTestDatabase(t)
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
