package test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrderedMigrationsPreservesExecutionOrder(t *testing.T) {
	got := migration.OrderedMigrationVersionsForTest()
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
	}
	if len(got) != len(want) {
		t.Fatalf("expected ordered migrations %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected ordered migrations %v, got %v", want, got)
		}
	}
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
	expected := []string{
		"20260503_add_usage_event_redis_fields",
		"20260503_backfill_usage_event_redis_fields",
		"20260503_drop_snapshot_runs",
		"20260504_backfill_usage_event_identity_fields",
		"20260504_backfill_usage_identity_stats",
		"20260504_create_usage_identities",
		"20260504_drop_legacy_metadata_tables",
		"20260504_drop_legacy_snapshot_run_columns",
		"20260504_migrate_usage_identities_metadata",
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
		"20260513_create_cpa_api_keys",
		"20260513_use_int64_primary_keys",
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
	}
	if len(versions) != len(expected) {
		t.Fatalf("expected migration versions %v, got %v", expected, versions)
	}
	for i := range expected {
		if versions[i] != expected[i] {
			t.Fatalf("expected migration versions %v, got %v", expected, versions)
		}
	}
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
	for _, version := range migration.OrderedMigrationVersionsForTest() {
		if version == "20260512_normalize_storage_times_to_project_tz" {
			continue
		}
		if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, "2026-05-12 13:47:39.744240399+00:00").Error; err != nil {
			t.Fatalf("seed schema_migrations %s: %v", version, err)
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

	if err := migration.Run(db); err != nil {
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
	expectedCount := int64(len(migration.OrderedMigrationVersionsForTest()))
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
		"msg=\"schema migration skipped\"",
		"version=20260503_add_usage_event_redis_fields",
		"version=20260504_migrate_usage_identities_metadata",
		"version=20260504_drop_legacy_metadata_tables",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected migration logs to contain %q, got:\n%s", want, content)
		}
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

func TestRunSchemaMigrationKeepsDefaultMigrationsTransactional(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "app.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	err = migration.RunSchemaMigrationForTest(db, "test_transactional_failure", func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE TABLE transactional_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
			return err
		}
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("expected migration error")
	}
	if db.Migrator().HasTable("transactional_probe") {
		t.Fatal("expected default schema migration to roll back created table")
	}
}

func TestRunSchemaMigrationLogsErrors(t *testing.T) {
	logs := captureMigrationLogs(t, logrus.InfoLevel)
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "app.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	err = migration.RunSchemaMigrationForTest(db, "test_failure", func(*gorm.DB) error {
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("expected migration error")
	}

	content := logs.String()
	for _, want := range []string{
		"level=info",
		"msg=\"schema migration started\"",
		"version=test_failure",
		"level=error",
		"msg=\"schema migration failed\"",
		"error=boom",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected migration error logs to contain %q, got:\n%s", want, content)
		}
	}
}

func openMigratedDatabase(t *testing.T, dbPath string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return db
}

func captureMigrationLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(level)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func closeOpenedDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func testSQLiteDSN(path string) string {
	trimmed := strings.TrimSpace(path)
	if strings.Contains(trimmed, "?") {
		return trimmed
	}
	return trimmed + "?_busy_timeout=5000&_foreign_keys=on"
}

func seedLegacyRedisUsageTables(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY AUTOINCREMENT,
			event_key text,
			snapshot_run_id integer,
			api_group_key text,
			model text,
			timestamp datetime,
			source text,
			auth_index text,
			failed numeric,
			latency_ms integer,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			total_tokens integer,
			created_at datetime
		)`,
		`CREATE UNIQUE INDEX uniq_usage_events_event_key ON usage_events(event_key)`,
		`CREATE TABLE redis_usage_inboxes (
			id integer PRIMARY KEY AUTOINCREMENT,
			queue_key text NOT NULL DEFAULT '',
			message_hash text NOT NULL DEFAULT '',
			raw_message text NOT NULL DEFAULT '',
			status text NOT NULL DEFAULT '',
			attempt_count integer NOT NULL DEFAULT 0,
			last_error text,
			snapshot_run_id integer,
			usage_event_key text,
			popped_at datetime NOT NULL DEFAULT '1970-01-01 00:00:00',
			processed_at datetime,
			created_at datetime,
			updated_at datetime
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy schema with %q: %v", statement, err)
		}
	}

	now := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	legacyEvents := []map[string]any{
		{"event_key": "legacy-canonical-key", "api_group_key": "raw-key", "model": "claude-sonnet", "timestamp": now, "created_at": now},
		{"event_key": "req-fallback", "api_group_key": "fallback", "model": "claude-opus", "timestamp": now, "created_at": now},
		{"event_key": "req-blank-fallback", "api_group_key": "blank", "model": "claude-opus", "timestamp": now, "created_at": now},
		{"event_key": "", "api_group_key": "empty", "model": "claude-empty", "timestamp": now, "created_at": now},
		{"event_key": "existing-key", "api_group_key": "existing", "model": "claude-haiku", "timestamp": now, "created_at": now},
	}
	for _, values := range legacyEvents {
		if err := db.Table("usage_events").Create(values).Error; err != nil {
			t.Fatalf("seed legacy usage event: %v", err)
		}
	}

	inboxes := []struct {
		hash          string
		rawMessage    string
		status        string
		usageEventKey string
		processedAt   *time.Time
	}{
		{hash: "hash-1", rawMessage: `{"provider":" claude ","endpoint":" /v1/messages ","auth_type":" API_KEY ","request_id":" req-from-raw "}`, status: "processed", usageEventKey: "legacy-canonical-key", processedAt: &now},
		{hash: "hash-2", rawMessage: `{"provider":" fallback-provider ","endpoint":" /fallback ","auth_type":" OAuth ","request_id":" req-fallback "}`, status: "processed", usageEventKey: "missing-key", processedAt: &now},
		{hash: "hash-3", rawMessage: `{"provider":" overwrite-provider ","endpoint":" /overwrite ","auth_type":" api_key ","request_id":" overwrite-request "}`, status: "processed", usageEventKey: "existing-key", processedAt: &now},
		{hash: "hash-4", rawMessage: `{"provider":" blank-provider ","endpoint":" /blank ","auth_type":" OAuth ","request_id":" req-blank-fallback "}`, status: "processed", usageEventKey: "", processedAt: &now},
		{hash: "hash-5", rawMessage: `{"provider":"pending-provider","request_id":"pending-key"}`, status: "pending", usageEventKey: "pending-key"},
	}
	for _, inbox := range inboxes {
		if err := db.Exec(
			"INSERT INTO redis_usage_inboxes (queue_key, message_hash, raw_message, status, attempt_count, usage_event_key, popped_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			cpa.ManagementUsageQueueKey, inbox.hash, inbox.rawMessage, inbox.status, 0, inbox.usageEventKey, now, inbox.processedAt, now, now,
		).Error; err != nil {
			t.Fatalf("seed legacy redis inbox: %v", err)
		}
	}
}
