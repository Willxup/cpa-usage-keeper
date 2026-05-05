package migration

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseDropsLegacySnapshotRunsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE snapshot_runs (id integer PRIMARY KEY AUTOINCREMENT, fetched_at datetime, status text)`).Error; err != nil {
		t.Fatalf("create legacy snapshot_runs table: %v", err)
	}
	if err := db.Exec(`INSERT INTO snapshot_runs (fetched_at, status) VALUES (?, ?)`, time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC), "completed").Error; err != nil {
		t.Fatalf("seed legacy snapshot_runs table: %v", err)
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if db.Migrator().HasTable("snapshot_runs") {
		t.Fatal("expected legacy snapshot_runs table to be dropped")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260503_drop_snapshot_runs").Count(&count).Error; err != nil {
		t.Fatalf("count drop snapshot migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected drop snapshot migration to be recorded once, got %d", count)
	}
}

func TestOpenDatabaseDropsLegacySnapshotRunIDColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if db.Migrator().HasColumn(&models.UsageEvent{}, "snapshot_run_id") {
		t.Fatal("expected usage_events.snapshot_run_id to be dropped")
	}
	if db.Migrator().HasColumn(&models.RedisUsageInbox{}, "snapshot_run_id") {
		t.Fatal("expected redis_usage_inboxes.snapshot_run_id to be dropped")
	}
	var oldIndexCount int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN (?, ?)", "idx_usage_events_snapshot_run_id", "idx_redis_usage_inboxes_snapshot_run_id").Scan(&oldIndexCount).Error; err != nil {
		t.Fatalf("count legacy snapshot_run_id indexes: %v", err)
	}
	if oldIndexCount != 0 {
		t.Fatalf("expected legacy snapshot_run_id indexes to be dropped, got %d", oldIndexCount)
	}
	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260504_drop_legacy_snapshot_run_columns").Count(&migrationCount).Error; err != nil {
		t.Fatalf("count drop snapshot_run_id columns migration: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected drop snapshot_run_id columns migration to be recorded once, got %d", migrationCount)
	}
}
