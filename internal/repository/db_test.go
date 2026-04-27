package repository

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func TestOpenDatabaseAutoMigratesCoreTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	cfg := config.Config{
		SQLitePath: dbPath,
	}

	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	if !db.Migrator().HasTable("snapshot_runs") {
		t.Fatal("expected snapshot_runs table to exist")
	}
	if !db.Migrator().HasTable("usage_events") {
		t.Fatal("expected usage_events table to exist")
	}
	if !db.Migrator().HasTable("redis_usage_inboxes") {
		t.Fatal("expected redis_usage_inboxes table to exist")
	}
}

func TestOpenDatabaseConfiguresSQLiteRuntime(t *testing.T) {
	db := openTestDatabase(t)

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("expected busy timeout at least 5000ms, got %d", busyTimeout)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign keys to be enabled, got %d", foreignKeys)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	if stats := sqlDB.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected sqlite max open connections to be 1, got %+v", stats)
	}
}

func TestCreateSnapshotRunStoresInitialState(t *testing.T) {
	db := openTestDatabase(t)
	fetchedAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	exportedAt := time.Date(2026, 4, 16, 11, 55, 0, 0, time.FixedZone("UTC+2", 2*60*60))

	run, err := CreateSnapshotRun(db, SnapshotRunInput{
		FetchedAt:    fetchedAt,
		CPABaseURL:   " https://cpa.example.com/ ",
		ExportedAt:   &exportedAt,
		Version:      "1",
		Status:       "pending",
		HTTPStatus:   200,
		PayloadHash:  "abc123",
		RawPayload:   []byte(`{"version":1}`),
		ErrorMessage: "",
	})
	if err != nil {
		t.Fatalf("CreateSnapshotRun returned error: %v", err)
	}

	var stored models.SnapshotRun
	if err := db.First(&stored, run.ID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if stored.Status != "pending" {
		t.Fatalf("expected pending status, got %q", stored.Status)
	}
	if stored.CPABaseURL != "https://cpa.example.com/" {
		t.Fatalf("expected trimmed base url, got %q", stored.CPABaseURL)
	}
	if stored.ExportedAt == nil || !stored.ExportedAt.Equal(exportedAt.UTC()) {
		t.Fatalf("expected normalized exported_at, got %+v", stored.ExportedAt)
	}
}

func TestInsertUsageEventsDeduplicatesByEventKey(t *testing.T) {
	db := openTestDatabase(t)
	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-1", SnapshotRunID: 2, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20},
	}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 2 || deduped != 1 {
		t.Fatalf("expected inserted=2 deduped=1, got inserted=%d deduped=%d", inserted, deduped)
	}

	var count int64
	if err := db.Model(&models.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 persisted usage events, got %d", count)
	}
}

func TestInsertUsageEventsBatchesLargeInsertSet(t *testing.T) {
	db := openTestDatabase(t)
	events := make([]models.UsageEvent, 0, 300)
	baseTime := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		events = append(events, models.UsageEvent{
			EventKey:      fmt.Sprintf("event-%03d", i),
			SnapshotRunID: 1,
			APIGroupKey:   "provider-a",
			Model:         "claude-sonnet",
			Timestamp:     baseTime.Add(time.Duration(i) * time.Minute),
			Source:        "source-a",
			AuthIndex:     "auth-1",
			TotalTokens:   int64(i + 1),
		})
	}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != len(events) || deduped != 0 {
		t.Fatalf("expected inserted=%d deduped=0, got inserted=%d deduped=%d", len(events), inserted, deduped)
	}

	var count int64
	if err := db.Model(&models.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != int64(len(events)) {
		t.Fatalf("expected %d persisted usage events, got %d", len(events), count)
	}
}

func TestFindLatestUsageEventTimestampReturnsNilForEmptyTable(t *testing.T) {
	db := openTestDatabase(t)

	timestamp, err := FindLatestUsageEventTimestamp(db)
	if err != nil {
		t.Fatalf("FindLatestUsageEventTimestamp returned error: %v", err)
	}
	if timestamp != nil {
		t.Fatalf("expected nil timestamp for empty table, got %v", *timestamp)
	}
}

func TestFindLatestUsageEventTimestampReturnsMaxValue(t *testing.T) {
	db := openTestDatabase(t)
	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 18, 11, 0, 0, 0, time.UTC), TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), TotalTokens: 15},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	timestamp, err := FindLatestUsageEventTimestamp(db)
	if err != nil {
		t.Fatalf("FindLatestUsageEventTimestamp returned error: %v", err)
	}
	if timestamp == nil {
		t.Fatal("expected max timestamp, got nil")
	}
	expected := time.Date(2026, 4, 18, 11, 0, 0, 0, time.UTC)
	if !timestamp.Equal(expected) {
		t.Fatalf("expected max timestamp %s, got %s", expected, timestamp)
	}
}

func TestFinalizeSnapshotRunUpdatesResultFields(t *testing.T) {
	db := openTestDatabase(t)
	run, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Now().UTC(), Status: "pending"})
	if err != nil {
		t.Fatalf("CreateSnapshotRun returned error: %v", err)
	}

	exportedAt := time.Date(2026, 4, 16, 12, 30, 0, 0, time.UTC)
	err = FinalizeSnapshotRun(db, run.ID, SnapshotRunResult{
		Status:         "completed",
		HTTPStatus:     200,
		BackupFilePath: "/tmp/export.json",
		InsertedEvents: 7,
		DedupedEvents:  2,
		ExportedAt:     &exportedAt,
	})
	if err != nil {
		t.Fatalf("FinalizeSnapshotRun returned error: %v", err)
	}

	var stored models.SnapshotRun
	if err := db.First(&stored, run.ID).Error; err != nil {
		t.Fatalf("load snapshot run: %v", err)
	}
	if stored.Status != "completed" {
		t.Fatalf("expected completed status, got %q", stored.Status)
	}
	if stored.InsertedEvents != 7 || stored.DedupedEvents != 2 {
		t.Fatalf("unexpected event counts: %+v", stored)
	}
	if stored.BackupFilePath != "/tmp/export.json" {
		t.Fatalf("expected backup path to be stored, got %q", stored.BackupFilePath)
	}
	if stored.ExportedAt == nil || !stored.ExportedAt.Equal(exportedAt) {
		t.Fatalf("expected exportedAt to be updated, got %+v", stored.ExportedAt)
	}
}

func TestFindLastSnapshotRunWithBackupReturnsLatestCompletedBackup(t *testing.T) {
	db := openTestDatabase(t)

	first, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Status: "pending"})
	if err != nil {
		t.Fatalf("CreateSnapshotRun first returned error: %v", err)
	}
	if err := FinalizeSnapshotRun(db, first.ID, SnapshotRunResult{Status: "completed", BackupFilePath: "/tmp/first.json"}); err != nil {
		t.Fatalf("FinalizeSnapshotRun first returned error: %v", err)
	}

	second, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC), Status: "pending"})
	if err != nil {
		t.Fatalf("CreateSnapshotRun second returned error: %v", err)
	}
	if err := FinalizeSnapshotRun(db, second.ID, SnapshotRunResult{Status: "completed"}); err != nil {
		t.Fatalf("FinalizeSnapshotRun second returned error: %v", err)
	}

	third, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Status: "pending"})
	if err != nil {
		t.Fatalf("CreateSnapshotRun third returned error: %v", err)
	}
	if err := FinalizeSnapshotRun(db, third.ID, SnapshotRunResult{Status: "completed", BackupFilePath: "/tmp/third.json"}); err != nil {
		t.Fatalf("FinalizeSnapshotRun third returned error: %v", err)
	}

	fourth, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC), Status: "pending"})
	if err != nil {
		t.Fatalf("CreateSnapshotRun fourth returned error: %v", err)
	}
	if err := FinalizeSnapshotRun(db, fourth.ID, SnapshotRunResult{Status: "completed_with_warnings", BackupFilePath: "/tmp/fourth.json"}); err != nil {
		t.Fatalf("FinalizeSnapshotRun fourth returned error: %v", err)
	}

	run, err := FindLastSnapshotRunWithBackup(db)
	if err != nil {
		t.Fatalf("FindLastSnapshotRunWithBackup returned error: %v", err)
	}
	if run == nil || run.ID != fourth.ID {
		t.Fatalf("expected latest successful backup snapshot %d, got %+v", fourth.ID, run)
	}
}

func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	return db
}
