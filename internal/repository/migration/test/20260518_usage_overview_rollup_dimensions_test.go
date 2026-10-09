package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUsageOverviewRollupDimensionsMigrationRebuildsRollupSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "overview-rollup-dimensions.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := runLegacyMigration(db, "20260514_create_usage_overview_stats"); err != nil {
		t.Fatalf("create overview stats: %v", err)
	}
	for _, unit := range []string{"hourly", "daily"} {
		if err := db.Exec("DROP INDEX uniq_usage_overview_" + unit + "_stats_bucket_api_model_auth_alias").Error; err != nil {
			t.Fatalf("drop %s unique index: %v", unit, err)
		}
		if err := db.Exec("CREATE UNIQUE INDEX uniq_usage_overview_" + unit + "_stats_bucket_api_model ON usage_overview_" + unit + "_stats (bucket_start, api_group_key, model)").Error; err != nil {
			t.Fatalf("create legacy %s unique index: %v", unit, err)
		}
	}
	seedUsageOverviewRollupTables(t, db)

	if err := runLegacyMigration(db, "20260518_usage_overview_rollup_dimensions"); err != nil {
		t.Fatalf("usageOverviewRollupDimensionsMigration returned error: %v", err)
	}
	if err := runLegacyMigration(db, "20260518_usage_overview_rollup_dimensions"); err != nil {
		t.Fatalf("usageOverviewRollupDimensionsMigration should be idempotent: %v", err)
	}

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, column := range []string{"auth_index", "model_alias"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("expected %s.%s column to exist", table, column)
			}
		}
	}
	for _, index := range []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_hourly_stats_auth_bucket",
		"idx_usage_overview_hourly_stats_model_alias_bucket",
		"uniq_usage_overview_daily_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_daily_stats_auth_bucket",
		"idx_usage_overview_daily_stats_model_alias_bucket",
	} {
		if !migrationSQLiteIndexExists(t, db, index) {
			t.Fatalf("expected index %s to exist", index)
		}
	}
	for _, index := range []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model",
		"uniq_usage_overview_daily_stats_bucket_api_model",
	} {
		if migrationSQLiteIndexExists(t, db, index) {
			t.Fatalf("expected legacy index %s to be removed", index)
		}
	}
	for _, table := range []string{
		"usage_overview_hourly_stats",
		"usage_overview_daily_stats",
		"usage_overview_health_stats",
		"usage_overview_aggregation_checkpoints",
	} {
		assertMigrationTableEmpty(t, db, table)
	}
}

func seedUsageOverviewRollupTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO usage_overview_hourly_stats (bucket_start, api_group_key, model, request_count, created_at, updated_at) VALUES ('2026-05-18 00:00:00', 'api-a', 'model-a', 1, '2026-05-18 00:00:00', '2026-05-18 00:00:00')`,
		`INSERT INTO usage_overview_daily_stats (bucket_start, api_group_key, model, request_count, created_at, updated_at) VALUES ('2026-05-18 00:00:00', 'api-a', 'model-a', 1, '2026-05-18 00:00:00', '2026-05-18 00:00:00')`,
		`INSERT INTO usage_overview_health_stats (bucket_start, span_seconds, api_group_key, success_count, failure_count, created_at, updated_at) VALUES ('2026-05-18 00:00:00', 900, 'api-a', 1, 0, '2026-05-18 00:00:00', '2026-05-18 00:00:00')`,
		`INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at) VALUES ('overview', 10, '2026-05-18 00:00:00', '2026-05-18 00:00:00', '2026-05-18 00:00:00')`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed overview rollup table: %v", err)
		}
	}
}

func assertMigrationTableEmpty(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != 0 {
		t.Fatalf("expected %s to be empty, got %d rows", table, count)
	}
}
