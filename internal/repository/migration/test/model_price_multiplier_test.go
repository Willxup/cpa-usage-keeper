package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const modelPriceMultiplierMigrationVersion = "20260702_model_price_multiplier"

func TestRunAddsModelPriceMultiplierToExistingPricing(t *testing.T) {
	db := openModelPriceMultiplierMigrationDatabase(t)
	defer closeModelPriceMultiplierMigrationDatabase(t, db)

	if err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)`).Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	for _, version := range previousModelPriceMultiplierMigrationVersions() {
		if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, "2026-07-01T00:00:00Z").Error; err != nil {
			t.Fatalf("seed schema migration %s: %v", version, err)
		}
	}
	if err := db.Exec(`CREATE TABLE model_price_settings (
		id integer PRIMARY KEY,
		model text,
		pricing_style text NOT NULL DEFAULT 'openai',
		prompt_price_per1_m real,
		completion_price_per1_m real,
		cache_price_per1_m real,
		cache_creation_price_per1_m real NOT NULL DEFAULT 0,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy model_price_settings table: %v", err)
	}
	if err := db.Exec(`INSERT INTO model_price_settings (
		id,
		model,
		pricing_style,
		prompt_price_per1_m,
		completion_price_per1_m,
		cache_price_per1_m,
		cache_creation_price_per1_m
	) VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "claude-sonnet", "claude", 3.0, 15.0, 0.3, 3.75).Error; err != nil {
		t.Fatalf("seed legacy model price setting: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn("model_price_settings", "price_multiplier") {
		t.Fatal("expected model_price_settings.price_multiplier column to exist after migration")
	}
	var multiplier float64
	if err := db.Table("model_price_settings").Select("price_multiplier").Where("model = ?", "claude-sonnet").Scan(&multiplier).Error; err != nil {
		t.Fatalf("load migrated price multiplier: %v", err)
	}
	if multiplier != 1 {
		t.Fatalf("expected legacy price multiplier to default to 1, got %v", multiplier)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", modelPriceMultiplierMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count model price multiplier migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration %s to be recorded once, got %d", modelPriceMultiplierMigrationVersion, count)
	}
}

func previousModelPriceMultiplierMigrationVersions() []string {
	return []string{
		"20260503_add_usage_event_redis_fields",
		"20260503_backfill_usage_event_redis_fields",
		"20260503_drop_snapshot_runs",
		"20260504_drop_legacy_snapshot_run_columns",
		"20260504_create_usage_identities",
		"20260504_migrate_usage_identities_metadata",
		"20260504_backfill_usage_event_identity_fields",
		"20260504_backfill_usage_identity_stats",
		"20260504_drop_legacy_metadata_tables",
		"20260504_remove_prefix_usage_identities",
		"20260505_add_usage_identity_lookup_key",
		"20260505_migrate_ai_provider_identities_to_auth_index",
		"20260506_add_usage_performance_indexes",
		"20260507_add_usage_identity_metadata_fields",
		"20260508_add_usage_event_model_alias",
		"20260509_update_usage_identity_quota_fields",
		"20260510_remove_usage_identity_quota_fields",
		"20260511_add_usage_identity_base_url",
		"20260512_normalize_storage_times_to_project_tz",
		"20260513_use_int64_primary_keys",
		"20260513_create_cpa_api_keys",
		"20260514_add_usage_event_cache_token_fields",
		"20260514_add_usage_event_plain_dimension_indexes",
		"20260514_create_usage_overview_stats",
		"20260514_remove_usage_event_event_key_unique_index",
		"20260517_add_usage_identity_sync_metadata_fields",
		"20260518_usage_overview_rollup_dimensions",
		"20260519_add_usage_event_reasoning_effort",
		"20260525_add_usage_event_quota_window_indexes",
		"20260528_add_usage_event_cpa_response_fields",
		"20260531_model_price_pricing_style",
		"20260601_backfill_claude_usage_tokens",
		"20260602_add_usage_event_executor_type",
		"20260603_add_usage_identity_file_fields",
		"20260605_backfill_gemini_codex_token_format",
		"20260610_remove_usage_event_write_heavy_indexes",
		"20260611_remove_usage_event_low_value_indexes",
		"20260612_replace_redis_inbox_queue_key_with_source",
		"20260620_create_auth_sessions",
		"20260629_add_usage_identity_alias",
		"20260701_add_auth_session_source",
	}
}

func openModelPriceMultiplierMigrationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "model-price-multiplier.db")), &gorm.Config{NowFunc: func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	return db
}

func closeModelPriceMultiplierMigrationDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sqlite database: %v", err)
	}
}
