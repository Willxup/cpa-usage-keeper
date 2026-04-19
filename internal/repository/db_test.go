package repository

import (
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

	run, err := FindLastSnapshotRunWithBackup(db)
	if err != nil {
		t.Fatalf("FindLastSnapshotRunWithBackup returned error: %v", err)
	}
	if run == nil || run.ID != third.ID {
		t.Fatalf("expected latest completed backup snapshot %d, got %+v", third.ID, run)
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
