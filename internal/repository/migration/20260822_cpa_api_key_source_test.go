package migration

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunAddsSourceToExistingCPAAPIKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "cpa-api-key-source.db"))), &gorm.Config{NowFunc: func() time.Time {
		return time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark all migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", migrationAddCPAAPIKeySource).Error; err != nil {
		t.Fatalf("mark target migration pending %s: %v", migrationAddCPAAPIKeySource, err)
	}
	if err := db.Exec(`CREATE TABLE cpa_api_keys (
		id INTEGER PRIMARY KEY,
		api_key TEXT,
		display_key TEXT,
		key_alias TEXT,
		is_deleted BOOLEAN,
		last_synced_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create legacy cpa_api_keys: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO cpa_api_keys (id, api_key, display_key, is_deleted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		int64(1),
		"sk-alpha123456",
		"sk-*********123456",
		false,
		"2026-08-21T00:00:00Z",
		"2026-08-21T00:00:00Z",
	).Error; err != nil {
		t.Fatalf("seed legacy cpa api key: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn(&entities.CPAAPIKey{}, "Source") {
		t.Fatal("expected cpa_api_keys.source column to exist after migration")
	}
	assertSQLiteColumnDefault(t, db, "cpa_api_keys", "source", `"cpa"`)
	var source string
	if err := db.Table("cpa_api_keys").Select("source").Where("api_key = ?", "sk-alpha123456").Scan(&source).Error; err != nil {
		t.Fatalf("load migrated source: %v", err)
	}
	if source != entities.CPAAPIKeySourceCPA {
		t.Fatalf("expected legacy key source to backfill to cpa, got %q", source)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migrationAddCPAAPIKeySource).Count(&count).Error; err != nil {
		t.Fatalf("count cpa api key source migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration %s to be recorded once, got %d", migrationAddCPAAPIKeySource, count)
	}
}
