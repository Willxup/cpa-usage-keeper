package test

import (
	"path/filepath"
	"slices"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateUsageOverviewStatsMigrationCreatesTablesAndIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "overview-stats.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := runLegacyMigration(db, "20260514_create_usage_overview_stats"); err != nil {
		t.Fatalf("create usage overview stats: %v", err)
	}
	if err := runLegacyMigration(db, "20260514_create_usage_overview_stats"); err != nil {
		t.Fatalf("create usage overview stats should be idempotent: %v", err)
	}

	rollupColumns := []string{
		"id", "bucket_start", "api_group_key", "model", "auth_index", "model_alias",
		"request_count", "success_count", "failure_count", "input_tokens", "output_tokens",
		"reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens",
		"total_tokens", "created_at", "updated_at",
	}
	for _, tc := range []struct {
		table   string
		columns []string
	}{
		{"usage_overview_hourly_stats", rollupColumns},
		{"usage_overview_daily_stats", rollupColumns},
		{"usage_overview_health_stats", []string{"id", "bucket_start", "span_seconds", "api_group_key", "success_count", "failure_count", "created_at", "updated_at"}},
		{"usage_overview_aggregation_checkpoints", []string{"id", "name", "last_aggregated_usage_event_id", "stats_updated_at", "created_at", "updated_at"}},
	} {
		columnTypes, err := db.Migrator().ColumnTypes(tc.table)
		if err != nil {
			t.Fatalf("load %s columns: %v", tc.table, err)
		}
		var names []string
		for _, column := range columnTypes {
			names = append(names, column.Name())
		}
		for _, column := range tc.columns {
			if !slices.Contains(names, column) {
				t.Fatalf("expected %s.%s column to exist", tc.table, column)
			}
		}
	}

	for _, index := range []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_hourly_stats_bucket_start",
		"idx_usage_overview_hourly_stats_api_bucket",
		"idx_usage_overview_hourly_stats_api_model_bucket",
		"idx_usage_overview_hourly_stats_auth_bucket",
		"idx_usage_overview_hourly_stats_model_alias_bucket",
		"uniq_usage_overview_daily_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_daily_stats_bucket_start",
		"idx_usage_overview_daily_stats_api_bucket",
		"idx_usage_overview_daily_stats_api_model_bucket",
		"idx_usage_overview_daily_stats_auth_bucket",
		"idx_usage_overview_daily_stats_model_alias_bucket",
		"uniq_usage_overview_health_stats_bucket_span_api",
		"idx_usage_overview_health_stats_bucket_start",
		"idx_usage_overview_health_stats_api_bucket_span",
		"uniq_usage_overview_aggregation_checkpoints_name",
	} {
		if !migrationSQLiteIndexExists(t, db, index) {
			t.Fatalf("expected index %s to exist", index)
		}
	}
}

func migrationSQLiteIndexExists(t *testing.T, db *gorm.DB, indexName string) bool {
	t.Helper()
	return sqliteIndexExists(t, db, indexName)
}
