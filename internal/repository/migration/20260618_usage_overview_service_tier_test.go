package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUsageOverviewServiceTierMigrationAddsTierColumnsAndClearsRollups(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "usage-overview-tier.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE usage_overview_hourly_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_start DATETIME NOT NULL,
			api_group_key TEXT NOT NULL,
			model TEXT NOT NULL,
			auth_index TEXT NOT NULL DEFAULT '',
			model_alias TEXT NOT NULL DEFAULT '',
			request_count INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE usage_overview_daily_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_start DATETIME NOT NULL,
			api_group_key TEXT NOT NULL,
			model TEXT NOT NULL,
			auth_index TEXT NOT NULL DEFAULT '',
			model_alias TEXT NOT NULL DEFAULT '',
			request_count INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE usage_overview_health_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket_start DATETIME NOT NULL,
			span_seconds INTEGER NOT NULL,
			api_group_key TEXT NOT NULL,
			success_count INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE usage_overview_aggregation_checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			last_aggregated_usage_event_id INTEGER NOT NULL DEFAULT 0,
			stats_updated_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE UNIQUE INDEX uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias ON usage_overview_hourly_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`CREATE UNIQUE INDEX uniq_usage_overview_daily_stats_bucket_api_model_auth_alias ON usage_overview_daily_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`INSERT INTO usage_overview_hourly_stats (bucket_start, api_group_key, model, auth_index, model_alias, request_count, created_at, updated_at)
		 VALUES ('2026-06-18 10:00:00', 'api-a', 'gpt-4o', 'auth-a', '', 3, '2026-06-18 10:00:00', '2026-06-18 10:00:00')`,
		`INSERT INTO usage_overview_daily_stats (bucket_start, api_group_key, model, auth_index, model_alias, request_count, created_at, updated_at)
		 VALUES ('2026-06-18 00:00:00', 'api-a', 'gpt-4o', 'auth-a', '', 4, '2026-06-18 00:00:00', '2026-06-18 00:00:00')`,
		`INSERT INTO usage_overview_health_stats (bucket_start, span_seconds, api_group_key, success_count, failure_count, created_at, updated_at)
		 VALUES ('2026-06-18 10:00:00', 600, 'api-a', 1, 0, '2026-06-18 10:00:00', '2026-06-18 10:00:00')`,
		`INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id, created_at, updated_at)
		 VALUES ('default', 99, '2026-06-18 10:00:00', '2026-06-18 10:00:00')`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy usage overview schema with %q: %v", stmt, err)
		}
	}

	if err := usageOverviewServiceTierMigration(db); err != nil {
		t.Fatalf("usageOverviewServiceTierMigration returned error: %v", err)
	}
	if err := usageOverviewServiceTierMigration(db); err != nil {
		t.Fatalf("usageOverviewServiceTierMigration should be idempotent: %v", err)
	}

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		if !db.Migrator().HasColumn(table, "service_tier") {
			t.Fatalf("expected %s.service_tier column to exist", table)
		}
	}
	if migrationSQLiteIndexExists(t, db, "uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias") {
		t.Fatal("expected legacy hourly unique index to be removed")
	}
	if migrationSQLiteIndexExists(t, db, "uniq_usage_overview_daily_stats_bucket_api_model_auth_alias") {
		t.Fatal("expected legacy daily unique index to be removed")
	}
	for _, index := range []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias",
		"uniq_usage_overview_daily_stats_bucket_api_model_tier_auth_alias",
	} {
		if !migrationSQLiteIndexExists(t, db, index) {
			t.Fatalf("expected index %s to exist", index)
		}
	}

	for _, table := range []string{
		"usage_overview_hourly_stats",
		"usage_overview_daily_stats",
		"usage_overview_health_stats",
		"usage_overview_aggregation_checkpoints",
	} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected %s to be cleared, got %d rows", table, count)
		}
	}

	if err := db.Exec(`INSERT INTO usage_overview_hourly_stats (bucket_start, api_group_key, model, auth_index, model_alias, request_count, created_at, updated_at)
		VALUES ('2026-06-18 11:00:00', 'api-a', 'gpt-4o', 'auth-a', '', 1, '2026-06-18 11:00:00', '2026-06-18 11:00:00')`).Error; err != nil {
		t.Fatalf("insert migrated hourly row: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_overview_daily_stats (bucket_start, api_group_key, model, auth_index, model_alias, request_count, created_at, updated_at)
		VALUES ('2026-06-19 00:00:00', 'api-a', 'gpt-4o', 'auth-a', '', 1, '2026-06-19 00:00:00', '2026-06-19 00:00:00')`).Error; err != nil {
		t.Fatalf("insert migrated daily row: %v", err)
	}

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var got string
		if err := db.Raw(`SELECT service_tier FROM ` + table + ` LIMIT 1`).Row().Scan(&got); err != nil {
			t.Fatalf("load %s.service_tier: %v", table, err)
		}
		if got != "default" {
			t.Fatalf("expected %s.service_tier default to be %q, got %q", table, "default", got)
		}
	}
}
