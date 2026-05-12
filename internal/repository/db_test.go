package repository

import (
	"bytes"
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
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
	closeTestDatabase(t, db)

	if db.Migrator().HasTable("snapshot_runs") {
		t.Fatal("expected legacy snapshot_runs table not to exist")
	}
	if !db.Migrator().HasTable("usage_events") {
		t.Fatal("expected usage_events table to exist")
	}
	if !db.Migrator().HasTable("redis_usage_inboxes") {
		t.Fatal("expected redis_usage_inboxes table to exist")
	}
}

func TestOpenDatabaseCreatesFreshDatabaseFromCurrentSchemaWithoutRunningMigrations(t *testing.T) {
	logs := captureRepositoryLogs(t)
	dbPath := filepath.Join(t.TempDir(), "app.db")

	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != 18 {
		t.Fatalf("expected fresh database to mark 18 migrations applied, got %d", count)
	}
	if strings.Contains(logs.String(), "schema migration started") {
		t.Fatalf("expected fresh database creation not to run version migrations, got logs:\n%s", logs.String())
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

func TestInsertUsageEventsDeduplicatesByEventKey(t *testing.T) {
	db := openTestDatabase(t)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 20},
	}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 2 || deduped != 1 {
		t.Fatalf("expected inserted=2 deduped=1, got inserted=%d deduped=%d", inserted, deduped)
	}

	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 persisted usage events, got %d", count)
	}
}

func TestInsertUsageEventsBatchesLargeInsertSet(t *testing.T) {
	db := openTestDatabase(t)
	events := make([]entities.UsageEvent, 0, 300)
	baseTime := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		events = append(events, entities.UsageEvent{
			EventKey:    fmt.Sprintf("event-%03d", i),
			APIGroupKey: "provider-a",
			Model:       "claude-sonnet",
			Timestamp:   baseTime.Add(time.Duration(i) * time.Minute),
			Source:      "source-a",
			AuthIndex:   "auth-1",
			TotalTokens: int64(i + 1),
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
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != int64(len(events)) {
		t.Fatalf("expected %d persisted usage events, got %d", len(events), count)
	}
}

func TestInsertUsageEventsPersistsModelAlias(t *testing.T) {
	db := openTestDatabase(t)
	modelAlias := "claude-sonnet-alias"
	events := []entities.UsageEvent{{
		EventKey:    "event-alias",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		ModelAlias:  &modelAlias,
		Timestamp:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		Source:      "source-a",
		AuthIndex:   "auth-1",
		TotalTokens: 10,
	}}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var got entities.UsageEvent
	if err := db.Where("event_key = ?", "event-alias").First(&got).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if got.ModelAlias == nil || *got.ModelAlias != "claude-sonnet-alias" {
		t.Fatalf("expected model alias persisted, got %+v", got.ModelAlias)
	}
}

func TestCleanupStorageCleansRedisInboxAndVacuums(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	inboxRows, err := InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"processed-old"}`, PoppedAt: now.AddDate(0, 0, -2)},
		{QueueKey: "queue", RawMessage: `{"request_id":"pending"}`, PoppedAt: now.AddDate(0, 0, -2)},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", inboxRows[0].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessed, "processed_at": time.Date(2026, 4, 26, 15, 59, 59, 0, time.UTC)}).Error; err != nil {
		t.Fatalf("seed processed inbox row: %v", err)
	}

	result, err := CleanupStorage(db, now)
	if err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}
	if result.RedisInbox.ProcessedDeleted != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	var inboxRemaining []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&inboxRemaining).Error; err != nil {
		t.Fatalf("load remaining inbox rows: %v", err)
	}
	if len(inboxRemaining) != 1 || inboxRemaining[0].ID != inboxRows[1].ID {
		t.Fatalf("expected only pending inbox row to remain, got %+v", inboxRemaining)
	}
}

func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
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

func captureRepositoryLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func repositorySQLiteIndexExists(t *testing.T, db *gorm.DB, indexName string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count).Error; err != nil {
		t.Fatalf("check sqlite index %s: %v", indexName, err)
	}
	return count == 1
}
