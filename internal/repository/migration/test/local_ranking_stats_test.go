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

const localRankingStatsMigrationVersion = "20260731_local_ranking_stats"

func TestLocalRankingStatsMigrationCreatesOnlyLightweightPeriodStorage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local-ranking.db")), &gorm.Config{})
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
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(
		"INSERT INTO cpa_api_keys (id, api_key, display_key, key_alias, is_deleted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		1, "sk-existing", "sk-***existing", "Existing", false, now, now,
	).Error; err != nil {
		t.Fatalf("seed existing CPA API key: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", localRankingStatsMigrationVersion).Error; err != nil {
		t.Fatalf("mark local ranking migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("run local ranking migration: %v", err)
	}
	if !db.Migrator().HasTable(&entities.LocalRankingPeriodStat{}) {
		t.Fatal("expected local ranking period stats table")
	}
	for _, column := range []string{"local_ranking_last_aggregated_usage_event_id", "local_ranking_stats_updated_at"} {
		if db.Migrator().HasColumn(&entities.CPAAPIKey{}, column) {
			t.Fatalf("did not expect obsolete cpa_api_keys.%s", column)
		}
	}
	var apiKey entities.CPAAPIKey
	if err := db.First(&apiKey, 1).Error; err != nil {
		t.Fatalf("reload migrated CPA API key: %v", err)
	}
	if apiKey.APIKey != "sk-existing" || apiKey.DisplayKey != "sk-***existing" || apiKey.KeyAlias != "Existing" {
		t.Fatalf("existing CPA API key changed during migration: %+v", apiKey)
	}
}
