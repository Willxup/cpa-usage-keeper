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

func TestCleanupSnapshotRunsKeepsLatestSnapshotPerLocalDayForSevenDays(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	oldDay, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC), RawPayload: []byte(`old`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun oldDay returned error: %v", err)
	}
	if _, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 20, 17, 0, 0, 0, time.UTC), RawPayload: []byte(`first-day-early`)}); err != nil {
		t.Fatalf("CreateSnapshotRun firstDayEarly returned error: %v", err)
	}
	firstDayLatest, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 21, 15, 30, 0, 0, time.UTC), RawPayload: []byte(`first-day-latest`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun firstDayLatest returned error: %v", err)
	}
	if _, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 26, 16, 10, 0, 0, time.UTC), RawPayload: []byte(`today-early`)}); err != nil {
		t.Fatalf("CreateSnapshotRun todayEarly returned error: %v", err)
	}
	todayLatest, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 27, 2, 0, 0, 0, time.UTC), RawPayload: []byte(`today-latest`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun todayLatest returned error: %v", err)
	}
	if _, _, err := InsertUsageEvents(db, []models.UsageEvent{{EventKey: "event-old-snapshot", SnapshotRunID: oldDay.ID, Timestamp: now, TotalTokens: 1}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	result, err := CleanupSnapshotRuns(db, now)
	if err != nil {
		t.Fatalf("CleanupSnapshotRuns returned error: %v", err)
	}
	if result.Deleted != 3 {
		t.Fatalf("expected 3 deleted snapshot runs, got %+v", result)
	}

	var remaining []models.SnapshotRun
	if err := db.Order("id asc").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining snapshot runs: %v", err)
	}
	remainingIDs := make([]uint, 0, len(remaining))
	for _, run := range remaining {
		remainingIDs = append(remainingIDs, run.ID)
	}
	expectedIDs := []uint{firstDayLatest.ID, todayLatest.ID}
	if fmt.Sprint(remainingIDs) != fmt.Sprint(expectedIDs) {
		t.Fatalf("expected remaining snapshot ids %v, got %v", expectedIDs, remainingIDs)
	}

	var eventCount int64
	if err := db.Model(&models.UsageEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected usage events to remain untouched, got %d", eventCount)
	}
}

func TestCleanupSnapshotRunsDeletesFutureSnapshots(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	kept, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 27, 2, 0, 0, 0, time.UTC), RawPayload: []byte(`kept`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun kept returned error: %v", err)
	}
	future, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 27, 4, 0, 0, 0, time.UTC), RawPayload: []byte(`future`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun future returned error: %v", err)
	}

	result, err := CleanupSnapshotRuns(db, now)
	if err != nil {
		t.Fatalf("CleanupSnapshotRuns returned error: %v", err)
	}
	if result.Deleted != 1 {
		t.Fatalf("expected future snapshot to be deleted, got %+v", result)
	}

	var remaining []models.SnapshotRun
	if err := db.Order("id asc").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining snapshot runs: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != kept.ID {
		t.Fatalf("expected only current snapshot %d to remain after deleting %d, got %+v", kept.ID, future.ID, remaining)
	}
}

func TestCleanupStorageCleansRedisInboxAndSnapshotRuns(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	inboxRows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"processed-old"}`, PoppedAt: now.AddDate(0, 0, -2)},
		{QueueKey: "queue", RawMessage: `{"request_id":"pending"}`, PoppedAt: now.AddDate(0, 0, -2)},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := db.Model(&models.RedisUsageInbox{}).Where("id = ?", inboxRows[0].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessed, "processed_at": time.Date(2026, 4, 26, 15, 59, 59, 0, time.UTC)}).Error; err != nil {
		t.Fatalf("seed processed inbox row: %v", err)
	}
	oldSnapshot, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC), RawPayload: []byte(`old`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun old returned error: %v", err)
	}
	keptSnapshot, err := CreateSnapshotRun(db, SnapshotRunInput{FetchedAt: time.Date(2026, 4, 27, 2, 0, 0, 0, time.UTC), RawPayload: []byte(`kept`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun kept returned error: %v", err)
	}

	result, err := CleanupStorage(db, now)
	if err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}
	if result.RedisInbox.ProcessedDeleted != 1 || result.SnapshotRuns.Deleted != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	var inboxRemaining []models.RedisUsageInbox
	if err := db.Order("id asc").Find(&inboxRemaining).Error; err != nil {
		t.Fatalf("load remaining inbox rows: %v", err)
	}
	if len(inboxRemaining) != 1 || inboxRemaining[0].ID != inboxRows[1].ID {
		t.Fatalf("expected only pending inbox row to remain, got %+v", inboxRemaining)
	}
	var snapshotRemaining []models.SnapshotRun
	if err := db.Order("id asc").Find(&snapshotRemaining).Error; err != nil {
		t.Fatalf("load remaining snapshot runs: %v", err)
	}
	if len(snapshotRemaining) != 1 || snapshotRemaining[0].ID != keptSnapshot.ID {
		t.Fatalf("expected only retained snapshot %d to remain after deleting %d, got %+v", keptSnapshot.ID, oldSnapshot.ID, snapshotRemaining)
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
