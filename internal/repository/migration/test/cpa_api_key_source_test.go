package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const cpaAPIKeySourceMigrationVersion = "20260807_add_cpa_api_key_source"

func TestCPAAPIKeySourceMigrationBackfillsExistingRowsAsNative(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cpa-api-key-source.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec(`CREATE TABLE cpa_api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key TEXT,
		display_key TEXT,
		key_alias TEXT,
		is_deleted NUMERIC,
		last_synced_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create old CPA API key table: %v", err)
	}
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(
		"INSERT INTO cpa_api_keys (api_key, display_key, key_alias, is_deleted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"sk-existing", "sk-***existing", "Existing", false, now, now,
	).Error; err != nil {
		t.Fatalf("seed existing CPA API key: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cpaAPIKeySourceMigrationVersion).Error; err != nil {
		t.Fatalf("mark CPA API key source migration pending: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run CPA API key source migration: %v", err)
	}

	var row entities.CPAAPIKey
	if err := db.Where("api_key = ?", "sk-existing").First(&row).Error; err != nil {
		t.Fatalf("reload migrated CPA API key: %v", err)
	}
	if row.Source != entities.CPAAPIKeySourceNative || row.APIKey != "sk-existing" || row.KeyAlias != "Existing" {
		t.Fatalf("migration changed existing metadata: %+v", row)
	}
	if !db.Migrator().HasIndex(&entities.CPAAPIKey{}, "idx_cpa_api_keys_source") {
		t.Fatal("expected CPA API key source index")
	}
}
