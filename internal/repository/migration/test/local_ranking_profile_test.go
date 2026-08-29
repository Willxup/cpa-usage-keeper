package test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const localRankingProfileMigrationVersion = "20260803_add_cpa_api_key_local_ranking_avatar"

func TestLocalRankingProfileMigrationAddsNullableAvatarWithoutChangingKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local-ranking-profile.db")), &gorm.Config{})
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
	now := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(
		"INSERT INTO cpa_api_keys (id, api_key, display_key, key_alias, is_deleted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		7, "sk-existing", "sk-***existing", "Existing", false, now, now,
	).Error; err != nil {
		t.Fatalf("seed existing CPA API key: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", localRankingProfileMigrationVersion).Error; err != nil {
		t.Fatalf("mark local ranking profile migration pending: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run local ranking profile migration: %v", err)
	}
	if !db.Migrator().HasColumn("cpa_api_keys", "local_ranking_avatar_id") {
		t.Fatal("expected nullable local ranking avatar column")
	}
	var row struct {
		APIKey               string
		KeyAlias             string
		LocalRankingAvatarID sql.NullInt64
	}
	if err := db.Table("cpa_api_keys").Where("id = ?", 7).First(&row).Error; err != nil {
		t.Fatalf("reload migrated CPA API key: %v", err)
	}
	if row.APIKey != "sk-existing" || row.KeyAlias != "Existing" || row.LocalRankingAvatarID.Valid {
		t.Fatalf("migration changed the existing key or assigned an override: %+v", row)
	}
}
