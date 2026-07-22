package migration

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrderedMigrationsPreservesExecutionOrder(t *testing.T) {
	got := make([]string, 0, len(orderedMigrations()))
	for _, migration := range orderedMigrations() {
		got = append(got, migration.version)
	}
	want := []string{
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
		"20260702_model_price_multiplier",
		"20260702_create_app_settings",
		"20260710_backfill_cache_read_tokens",
		"20260711_add_usage_identity_xai_user_id",
		"20260715_add_usage_event_response_service_tier",
		"20260715_add_usage_event_generate",
		// Activity 必须在所有 usage_events 字段规范化 migration 之后回填。
		"20260719_usage_activity_stats",
		"20260722_align_usage_activity_short",
		"20260723_usage_overview_five_dimensions",
	}
	assertStringSlicesEqual(t, want, got)
}

func TestOpenDatabaseRunsSchemaMigrationsAndAddsUsageEventRedisFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasTable("schema_migrations") {
		t.Fatal("expected schema_migrations table to exist")
	}
	for _, column := range []string{"provider", "endpoint", "auth_type", "request_id", "executor_type"} {
		if !db.Migrator().HasColumn(&entities.UsageEvent{}, column) {
			t.Fatalf("expected usage_events.%s column to exist", column)
		}
	}
	if !db.Migrator().HasColumn(&entities.RedisUsageInbox{}, "source") {
		t.Fatal("expected redis_usage_inboxes.source column to exist")
	}
	if !db.Migrator().HasTable(&entities.AuthSession{}) {
		t.Fatal("expected auth_sessions table to exist")
	}
	if !db.Migrator().HasColumn(&entities.AuthSession{}, "token_hash") {
		t.Fatal("expected auth_sessions.token_hash column to exist")
	}
	if db.Migrator().HasColumn(&entities.AuthSession{}, "token") {
		t.Fatal("expected auth_sessions.token column not to exist")
	}
	if !db.Migrator().HasColumn(&entities.AuthSession{}, "expires_at") {
		t.Fatal("expected auth_sessions.expires_at column to exist")
	}
	if !db.Migrator().HasColumn(&entities.AuthSession{}, "source") {
		t.Fatal("expected auth_sessions.source column to exist")
	}
	if !db.Migrator().HasTable(&entities.AppSetting{}) {
		t.Fatal("expected app_settings table to exist")
	}
	if db.Migrator().HasColumn(&entities.RedisUsageInbox{}, "queue_key") {
		t.Fatal("expected redis_usage_inboxes.queue_key column not to exist")
	}
	if !db.Migrator().HasColumn(&entities.UsageIdentity{}, "lookup_key") {
		t.Fatal("expected usage_identities.lookup_key column to exist")
	}
	for _, column := range []string{"file_name", "file_path", "alias"} {
		if !db.Migrator().HasColumn(&entities.UsageIdentity{}, column) {
			t.Fatalf("expected usage_identities.%s column to exist", column)
		}
	}
	var versions []string
	if err := db.Table("schema_migrations").Order("version asc").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("load schema migrations: %v", err)
	}
	assertStringSlicesEqual(t, sortedOrderedMigrationVersions(), versions)
}

func TestRunNormalizesLegacyStorageTimesToProjectTimezone(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy-times.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if err := db.Exec("CREATE TABLE usage_events (id INTEGER PRIMARY KEY AUTOINCREMENT, event_key TEXT, model TEXT, timestamp DATETIME, created_at DATETIME)").Error; err != nil {
		t.Fatalf("create usage_events: %v", err)
	}
	if err := db.Exec("CREATE TABLE redis_usage_inboxes (id INTEGER PRIMARY KEY AUTOINCREMENT, popped_at DATETIME NOT NULL, processed_at DATETIME, created_at DATETIME, updated_at DATETIME)").Error; err != nil {
		t.Fatalf("create redis_usage_inboxes: %v", err)
	}
	if err := db.Exec("CREATE TABLE usage_identities (id INTEGER PRIMARY KEY AUTOINCREMENT, identity TEXT, active_start DATETIME, active_until DATETIME, first_used_at DATETIME, last_used_at DATETIME, stats_updated_at DATETIME, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)").Error; err != nil {
		t.Fatalf("create usage_identities: %v", err)
	}
	if err := db.Exec("CREATE TABLE model_price_settings (id INTEGER PRIMARY KEY AUTOINCREMENT, model TEXT, created_at DATETIME, updated_at DATETIME)").Error; err != nil {
		t.Fatalf("create model_price_settings: %v", err)
	}
	for _, migration := range orderedMigrations() {
		if migration.version == migrationNormalizeStorageTimesToProjectTZ {
			continue
		}
		if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", migration.version, "2026-05-12 13:47:39.744240399+00:00").Error; err != nil {
			t.Fatalf("seed schema_migrations %s: %v", migration.version, err)
		}
	}
	if err := db.Exec("INSERT INTO usage_events (event_key, model, timestamp, created_at) VALUES (?, ?, ?, ?)", "event-1", "claude-sonnet", "2026-05-12 13:47:39.744240399+00:00", "2026-05-12T13:47:39.744240399Z").Error; err != nil {
		t.Fatalf("seed usage_events: %v", err)
	}
	if err := db.Exec("INSERT INTO redis_usage_inboxes (popped_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?)", "2026-05-12T21:47:39.744240399+08:00", "2026-05-12 13:47:39.744240399", "2026-05-12T13:47:39.744240399", "2026-05-12 13:47:39").Error; err != nil {
		t.Fatalf("seed redis_usage_inboxes: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_identities (identity, active_start, active_until, first_used_at, last_used_at, stats_updated_at, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "auth-1", "2026-05-12 13:47:39.744240399", "2026-05-12T13:47:39.744240399", "2026-05-12T13:47:39.744240399Z", "2026-05-12 13:47:39.744240399+00:00", "2026-05-12T21:47:39.744240399+08:00", "2026-05-12 13:47:39", "2026-05-12T13:47:39", nil).Error; err != nil {
		t.Fatalf("seed usage_identities: %v", err)
	}
	if err := db.Exec("INSERT INTO model_price_settings (model, created_at, updated_at) VALUES (?, ?, ?)", "claude-sonnet", "2026-05-12T13:47:39.744240399Z", "2026-05-12 13:47:39.744240399").Error; err != nil {
		t.Fatalf("seed model_price_settings: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	assertRawMigrationTime(t, db, "usage_events", "timestamp", "id = 1", "2026-05-12T21:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "usage_events", "created_at", "id = 1", "2026-05-12T21:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "redis_usage_inboxes", "popped_at", "id = 1", "2026-05-12T21:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "redis_usage_inboxes", "processed_at", "id = 1", "2026-05-12T13:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "usage_identities", "active_start", "id = 1", "2026-05-12T13:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "usage_identities", "first_used_at", "id = 1", "2026-05-12T21:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "usage_identities", "deleted_at", "id = 1", "")
	assertRawMigrationTime(t, db, "model_price_settings", "updated_at", "id = 1", "2026-05-12T13:47:39.744240399+08:00")
	assertRawMigrationTime(t, db, "schema_migrations", "applied_at", "version = '20260511_add_usage_identity_base_url'", "2026-05-12T21:47:39.744240399+08:00")
}

func TestOpenDatabaseMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	expectedCount := int64(len(orderedMigrations()))
	if count != expectedCount {
		t.Fatalf("expected %d applied migrations after reopening database, got %d", expectedCount, count)
	}
}

func TestOpenDatabaseLogsSchemaMigrations(t *testing.T) {
	logs := captureMigrationLogs(t, logrus.InfoLevel)
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)

	content := logs.String()
	for _, want := range []string{
		"level=info",
		"msg=\"schema migration started\"",
		"msg=\"schema migration applied\"",
		"version=20260503_add_usage_event_redis_fields",
		"version=20260504_migrate_usage_identities_metadata",
		"version=20260504_drop_legacy_metadata_tables",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected migration logs to contain %q, got:\n%s", want, content)
		}
	}
	if strings.Contains(content, "msg=\"schema migration skipped\"") {
		t.Fatalf("expected info logs to hide skipped migrations, got:\n%s", content)
	}

	logrus.SetLevel(logrus.DebugLevel)
	db = openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)

	content = logs.String()
	want := "level=debug msg=\"schema migration skipped\" version=20260503_add_usage_event_redis_fields"
	if !strings.Contains(content, want) {
		t.Fatalf("expected debug migration logs to contain %q, got:\n%s", want, content)
	}
}

func assertRawMigrationTime(t *testing.T, db *gorm.DB, table string, field string, where string, want string) {
	t.Helper()
	var got *string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT 1", field, table, where)).Scan(&got).Error; err != nil {
		t.Fatalf("read %s.%s: %v", table, field, err)
	}
	if want == "" {
		if got != nil {
			t.Fatalf("expected %s.%s to stay NULL, got %q", table, field, *got)
		}
		return
	}
	if got == nil || *got != want {
		if got == nil {
			t.Fatalf("expected %s.%s = %q, got NULL", table, field, want)
		}
		t.Fatalf("expected %s.%s = %q, got %q", table, field, want, *got)
	}
}

func TestRunKeepsDefaultMigrationsTransactionalWhenRecordingFails(t *testing.T) {
	db := openDatabaseWithFailingMigrationRecord(t, "20260620_create_auth_sessions")
	defer closeOpenedDatabase(t, db)

	err := Run(db)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if db.Migrator().HasTable(&entities.AuthSession{}) {
		t.Fatal("expected failed auth session migration to roll back created table")
	}
}

func TestRunLogsSchemaMigrationErrors(t *testing.T) {
	logs := captureMigrationLogs(t, logrus.InfoLevel)
	db := openDatabaseWithFailingMigrationRecord(t, "20260620_create_auth_sessions")
	defer closeOpenedDatabase(t, db)

	err := Run(db)
	if err == nil {
		t.Fatal("expected migration error")
	}

	content := logs.String()
	for _, want := range []string{
		"level=info",
		"msg=\"schema migration started\"",
		"version=20260620_create_auth_sessions",
		"level=error",
		"msg=\"schema migration failed\"",
		"error=\"forced record failure\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected migration error logs to contain %q, got:\n%s", want, content)
		}
	}
}

func TestRunAddsSourceToExistingAuthSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "auth-session-source.db"))), &gorm.Config{NowFunc: func() time.Time {
		return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark all migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", migrationAddAuthSessionSource).Error; err != nil {
		t.Fatalf("mark target migration pending %s: %v", migrationAddAuthSessionSource, err)
	}
	if err := db.Exec(`CREATE TABLE auth_sessions (
		token_hash TEXT PRIMARY KEY,
		role TEXT NOT NULL,
		cpa_api_key_id INTEGER,
		expires_at DATETIME NOT NULL,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create legacy auth_sessions: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO auth_sessions (token_hash, role, cpa_api_key_id, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"legacy-token-hash",
		"admin",
		0,
		"2026-07-02T00:00:00Z",
		"2026-07-01T00:00:00Z",
		"2026-07-01T00:00:00Z",
	).Error; err != nil {
		t.Fatalf("seed legacy auth session: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn(&entities.AuthSession{}, "Source") {
		t.Fatal("expected auth_sessions.source column to exist after migration")
	}
	var source string
	if err := db.Table("auth_sessions").Select("source").Where("token_hash = ?", "legacy-token-hash").Scan(&source).Error; err != nil {
		t.Fatalf("load migrated source: %v", err)
	}
	if source != "standard" {
		t.Fatalf("expected legacy session source to backfill to standard, got %q", source)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migrationAddAuthSessionSource).Count(&count).Error; err != nil {
		t.Fatalf("count auth session source migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration %s to be recorded once, got %d", migrationAddAuthSessionSource, count)
	}
}

func TestRunAddsModelPriceMultiplierDefaultToExistingPricing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "model-price-multiplier.db"))), &gorm.Config{NowFunc: func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark all migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", migrationModelPriceMultiplier).Error; err != nil {
		t.Fatalf("mark target migration pending %s: %v", migrationModelPriceMultiplier, err)
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

	if err := Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn("model_price_settings", "price_multiplier") {
		t.Fatal("expected model_price_settings.price_multiplier column to exist after migration")
	}
	assertSQLiteColumnDefault(t, db, "model_price_settings", "price_multiplier", "1")
	var multiplier float64
	if err := db.Table("model_price_settings").Select("price_multiplier").Where("model = ?", "claude-sonnet").Scan(&multiplier).Error; err != nil {
		t.Fatalf("load migrated price multiplier: %v", err)
	}
	if multiplier != 1 {
		t.Fatalf("expected legacy price multiplier to default to 1, got %v", multiplier)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migrationModelPriceMultiplier).Count(&count).Error; err != nil {
		t.Fatalf("count model price multiplier migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration %s to be recorded once, got %d", migrationModelPriceMultiplier, count)
	}
}

func TestRunBackfillsModelPriceMultiplierWhenColumnAlreadyExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "model-price-multiplier-existing-column.db"))), &gorm.Config{NowFunc: func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark all migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", migrationModelPriceMultiplier).Error; err != nil {
		t.Fatalf("mark target migration pending %s: %v", migrationModelPriceMultiplier, err)
	}
	if err := db.Exec(`CREATE TABLE model_price_settings (
		id integer PRIMARY KEY,
		model text,
		pricing_style text NOT NULL DEFAULT 'openai',
		prompt_price_per1_m real,
		completion_price_per1_m real,
		cache_price_per1_m real,
		cache_creation_price_per1_m real NOT NULL DEFAULT 0,
		price_multiplier real,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create partially migrated model_price_settings table: %v", err)
	}
	if err := db.Exec(`INSERT INTO model_price_settings (
		id,
		model,
		pricing_style,
		prompt_price_per1_m,
		completion_price_per1_m,
		cache_price_per1_m,
		cache_creation_price_per1_m,
		price_multiplier
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(1), "legacy-null", "openai", 3.0, 15.0, 0.3, 0.0, nil,
		int64(2), "explicit-zero", "openai", 3.0, 15.0, 0.3, 0.0, 0.0,
	).Error; err != nil {
		t.Fatalf("seed partially migrated model price settings: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var nullBackfill *float64
	if err := db.Table("model_price_settings").Select("price_multiplier").Where("model = ?", "legacy-null").Scan(&nullBackfill).Error; err != nil {
		t.Fatalf("load null multiplier backfill: %v", err)
	}
	if nullBackfill == nil || *nullBackfill != 1 {
		t.Fatalf("expected NULL price multiplier to backfill to 1, got %+v", nullBackfill)
	}
	var zeroMultiplier *float64
	if err := db.Table("model_price_settings").Select("price_multiplier").Where("model = ?", "explicit-zero").Scan(&zeroMultiplier).Error; err != nil {
		t.Fatalf("load explicit zero multiplier: %v", err)
	}
	if zeroMultiplier == nil || *zeroMultiplier != 0 {
		t.Fatalf("expected explicit zero price multiplier to stay 0, got %+v", zeroMultiplier)
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migrationModelPriceMultiplier).Count(&count).Error; err != nil {
		t.Fatalf("count model price multiplier migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration %s to be recorded once, got %d", migrationModelPriceMultiplier, count)
	}
}

func openDatabaseWithFailingMigrationRecord(t *testing.T, version string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "app.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	seedSchemaMigrationsBefore(t, db, version, "2026-06-20T00:00:00Z")
	quotedVersion := strings.ReplaceAll(version, "'", "''")
	if err := db.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_schema_migration_record
		BEFORE INSERT ON schema_migrations
		WHEN NEW.version = '%s'
		BEGIN
			SELECT RAISE(ABORT, 'forced record failure');
		END
	`, quotedVersion)).Error; err != nil {
		t.Fatalf("create schema migration failure trigger: %v", err)
	}
	return db
}

func seedSchemaMigrationsBefore(t *testing.T, db *gorm.DB, targetVersion string, appliedAt string) {
	t.Helper()
	for _, migration := range orderedMigrations() {
		if migration.version == targetVersion {
			return
		}
		if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", migration.version, appliedAt).Error; err != nil {
			t.Fatalf("seed schema_migrations %s: %v", migration.version, err)
		}
	}
	t.Fatalf("missing migration version %s in expected migration list", targetVersion)
}

func sortedOrderedMigrationVersions() []string {
	versions := make([]string, 0, len(orderedMigrations()))
	for _, migration := range orderedMigrations() {
		versions = append(versions, migration.version)
	}
	sort.Strings(versions)
	return versions
}

func assertSQLiteColumnDefault(t *testing.T, db *gorm.DB, table string, column string, wantDefault string) {
	t.Helper()
	type columnInfo struct {
		Name       string
		DefaultRaw *string `gorm:"column:dflt_value"`
	}
	var columns []columnInfo
	if err := db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", table)).Scan(&columns).Error; err != nil {
		t.Fatalf("read sqlite table info for %s: %v", table, err)
	}
	for _, info := range columns {
		if info.Name != column {
			continue
		}
		if info.DefaultRaw == nil || *info.DefaultRaw != wantDefault {
			if info.DefaultRaw == nil {
				t.Fatalf("expected %s.%s default %q, got NULL", table, column, wantDefault)
			}
			t.Fatalf("expected %s.%s default %q, got %q", table, column, wantDefault, *info.DefaultRaw)
		}
		return
	}
	t.Fatalf("expected %s.%s column in sqlite table info, got %+v", table, column, columns)
}

func assertStringSlicesEqual(t *testing.T, want []string, got []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected versions %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected versions %v, got %v", want, got)
		}
	}
}
