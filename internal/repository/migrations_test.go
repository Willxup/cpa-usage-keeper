package repository

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseRunsSchemaMigrationsAndAddsUsageEventRedisFields(t *testing.T) {
	db := openTestDatabase(t)

	if !db.Migrator().HasTable("schema_migrations") {
		t.Fatal("expected schema_migrations table to exist")
	}
	for _, column := range []string{"provider", "endpoint", "auth_type", "request_id"} {
		if !db.Migrator().HasColumn(&models.UsageEvent{}, column) {
			t.Fatalf("expected usage_events.%s column to exist", column)
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

func TestOpenDatabaseMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	cfg := config.Config{SQLitePath: dbPath}

	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("first OpenDatabase returned error: %v", err)
	}
	closeOpenedDatabase(t, db)

	db, err = OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("second OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 applied migrations after reopening database, got %d", count)
	}
}

func TestOpenDatabaseLogsSchemaMigrations(t *testing.T) {
	logs := captureRepositoryLogs(t, logrus.InfoLevel)
	dbPath := filepath.Join(t.TempDir(), "app.db")
	cfg := config.Config{SQLitePath: dbPath}

	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("first OpenDatabase returned error: %v", err)
	}
	closeOpenedDatabase(t, db)

	db, err = OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("second OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

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

func TestRunSchemaMigrationLogsErrors(t *testing.T) {
	logs := captureRepositoryLogs(t, logrus.InfoLevel)
	db, err := gorm.Open(sqlite.Open(sqliteDSN(filepath.Join(t.TempDir(), "app.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	err = runSchemaMigration(db, databaseMigration{
		version: "test_failure",
		run: func(*gorm.DB) error {
			return fmt.Errorf("boom")
		},
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

func TestOpenDatabaseDropsLegacySnapshotRunsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE snapshot_runs (id integer PRIMARY KEY AUTOINCREMENT, fetched_at datetime, status text)`).Error; err != nil {
		t.Fatalf("create legacy snapshot_runs table: %v", err)
	}
	if err := db.Exec(`INSERT INTO snapshot_runs (fetched_at, status) VALUES (?, ?)`, time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC), "completed").Error; err != nil {
		t.Fatalf("seed legacy snapshot_runs table: %v", err)
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if db.Migrator().HasTable("snapshot_runs") {
		t.Fatal("expected legacy snapshot_runs table to be dropped")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260503_drop_snapshot_runs").Count(&count).Error; err != nil {
		t.Fatalf("count drop snapshot migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected drop snapshot migration to be recorded once, got %d", count)
	}
}

func TestOpenDatabaseDropsLegacySnapshotRunIDColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if db.Migrator().HasColumn(&models.UsageEvent{}, "snapshot_run_id") {
		t.Fatal("expected usage_events.snapshot_run_id to be dropped")
	}
	if db.Migrator().HasColumn(&models.RedisUsageInbox{}, "snapshot_run_id") {
		t.Fatal("expected redis_usage_inboxes.snapshot_run_id to be dropped")
	}
	var oldIndexCount int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN (?, ?)", "idx_usage_events_snapshot_run_id", "idx_redis_usage_inboxes_snapshot_run_id").Scan(&oldIndexCount).Error; err != nil {
		t.Fatalf("count legacy snapshot_run_id indexes: %v", err)
	}
	if oldIndexCount != 0 {
		t.Fatalf("expected legacy snapshot_run_id indexes to be dropped, got %d", oldIndexCount)
	}
	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260504_drop_legacy_snapshot_run_columns").Count(&migrationCount).Error; err != nil {
		t.Fatalf("count drop snapshot_run_id columns migration: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected drop snapshot_run_id columns migration to be recorded once, got %d", migrationCount)
	}
}

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByUsageEventKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-canonical-key").First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "req-from-raw" {
		t.Fatalf("expected backfill by usage_event_key, got %+v", event)
	}
}

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByRawRequestIDFallback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "req-fallback").First(&event).Error; err != nil {
		t.Fatalf("load fallback usage event: %v", err)
	}
	if event.Provider != "fallback-provider" || event.Endpoint != "/fallback" || event.AuthType != "oauth" || event.RequestID != "req-fallback" {
		t.Fatalf("expected fallback backfill by raw request_id, got %+v", event)
	}
}

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByRawRequestIDWhenUsageEventKeyIsBlank(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "req-blank-fallback").First(&event).Error; err != nil {
		t.Fatalf("load blank fallback usage event: %v", err)
	}
	if event.Provider != "blank-provider" || event.Endpoint != "/blank" || event.AuthType != "oauth" || event.RequestID != "req-blank-fallback" {
		t.Fatalf("expected blank usage_event_key to fall back by raw request_id, got %+v", event)
	}

	var emptyEvent models.UsageEvent
	if err := db.Where("event_key = ?", "").First(&emptyEvent).Error; err != nil {
		t.Fatalf("load empty-key usage event: %v", err)
	}
	if emptyEvent.Provider != "" || emptyEvent.Endpoint != "" || emptyEvent.AuthType != "" || emptyEvent.RequestID != "" {
		t.Fatalf("expected empty-key usage event to remain unchanged, got %+v", emptyEvent)
	}
}

func TestOpenDatabaseBackfillDoesNotOverwriteExistingUsageEventFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	// 模拟目标列已经有值的部分迁移数据库。
	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open partially migrated database: %v", err)
	}
	for _, statement := range []string{
		"ALTER TABLE usage_events ADD COLUMN provider TEXT",
		"ALTER TABLE usage_events ADD COLUMN endpoint TEXT",
		"ALTER TABLE usage_events ADD COLUMN auth_type TEXT",
		"ALTER TABLE usage_events ADD COLUMN request_id TEXT",
		"UPDATE usage_events SET provider = 'existing-provider', endpoint = 'existing-endpoint', auth_type = 'existing-auth', request_id = 'existing-request' WHERE event_key = 'existing-key'",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare partially migrated database with %q: %v", statement, err)
		}
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "existing-key").First(&event).Error; err != nil {
		t.Fatalf("load existing usage event: %v", err)
	}
	if event.Provider != "existing-provider" || event.Endpoint != "existing-endpoint" || event.AuthType != "existing-auth" || event.RequestID != "existing-request" {
		t.Fatalf("expected existing fields to remain unchanged, got %+v", event)
	}
}

func TestOpenDatabaseUsageIdentityMigratesLegacyMetadataAndDropsOldTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasTable(&models.UsageIdentity{}) {
		t.Fatal("expected usage_identities table to exist")
	}
	if db.Migrator().HasTable("auth_files") {
		t.Fatal("expected auth_files table to be dropped")
	}
	if db.Migrator().HasTable("provider_metadata") {
		t.Fatal("expected provider_metadata table to be dropped")
	}

	var identities []models.UsageIdentity
	if err := db.Order("auth_type asc, identity asc").Find(&identities).Error; err != nil {
		t.Fatalf("load usage identities: %v", err)
	}
	if len(identities) != 4 {
		t.Fatalf("expected all 4 legacy usage identities, got %d: %+v", len(identities), identities)
	}

	oauth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-1")
	if oauth.Name != "person@example.com" || oauth.AuthTypeName != "oauth" || oauth.Type != "claude" || oauth.Provider != "claude" {
		t.Fatalf("unexpected oauth identity mapping: %+v", oauth)
	}
	if oauth.TotalRequests != 3 || oauth.SuccessCount != 2 || oauth.FailureCount != 1 || oauth.InputTokens != 31 || oauth.OutputTokens != 41 || oauth.ReasoningTokens != 11 || oauth.CachedTokens != 7 || oauth.TotalTokens != 90 {
		t.Fatalf("unexpected oauth identity stats: %+v", oauth)
	}
	if oauth.FirstUsedAt == nil || !oauth.FirstUsedAt.Equal(time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected oauth first used timestamp: %+v", oauth.FirstUsedAt)
	}
	if oauth.LastUsedAt == nil || !oauth.LastUsedAt.Equal(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected oauth last used timestamp: %+v", oauth.LastUsedAt)
	}
	if oauth.StatsUpdatedAt == nil {
		t.Fatal("expected oauth stats_updated_at to be set")
	}
	if oauth.LastAggregatedUsageEventID != 3 {
		t.Fatalf("expected oauth last aggregated usage event id 3, got %d", oauth.LastAggregatedUsageEventID)
	}

	provider := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAIProvider, "api-source-1")
	if provider.Name != "Claude API" || provider.AuthTypeName != "apikey" || provider.Type != "claude" || provider.Provider != "Claude API" {
		t.Fatalf("unexpected provider identity mapping: %+v", provider)
	}
	if provider.TotalRequests != 2 || provider.SuccessCount != 2 || provider.FailureCount != 0 || provider.InputTokens != 9 || provider.OutputTokens != 9 || provider.ReasoningTokens != 10 || provider.CachedTokens != 11 || provider.TotalTokens != 39 {
		t.Fatalf("unexpected provider identity stats: %+v", provider)
	}
	if provider.FirstUsedAt == nil || !provider.FirstUsedAt.Equal(time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)) || provider.LastUsedAt == nil || !provider.LastUsedAt.Equal(time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("unexpected provider usage timestamps: first=%+v last=%+v", provider.FirstUsedAt, provider.LastUsedAt)
	}
	if provider.StatsUpdatedAt == nil {
		t.Fatal("expected provider stats_updated_at to be set")
	}
	if provider.LastAggregatedUsageEventID != 5 {
		t.Fatalf("expected provider last aggregated usage event id 5, got %d", provider.LastAggregatedUsageEventID)
	}

	deletedOAuth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-deleted")
	if !deletedOAuth.IsDeleted || deletedOAuth.DeletedAt == nil || !deletedOAuth.DeletedAt.Equal(time.Date(2026, 5, 4, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected deleted auth file state to be preserved, got %+v", deletedOAuth)
	}
	if deletedOAuth.TotalRequests != 1 || deletedOAuth.TotalTokens != 100 || deletedOAuth.LastAggregatedUsageEventID != 6 {
		t.Fatalf("expected deleted auth file usage stats to be backfilled, got %+v", deletedOAuth)
	}

	deletedProvider := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAIProvider, "api-deleted")
	if !deletedProvider.IsDeleted || deletedProvider.DeletedAt == nil || !deletedProvider.DeletedAt.Equal(time.Date(2026, 5, 4, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected deleted provider state to be preserved, got %+v", deletedProvider)
	}
	if deletedProvider.TotalRequests != 1 || deletedProvider.TotalTokens != 100 || deletedProvider.LastAggregatedUsageEventID != 7 {
		t.Fatalf("expected deleted provider usage stats to be backfilled, got %+v", deletedProvider)
	}
}

func TestOpenDatabaseBackfillsUsageEventIdentityFieldsFromUsageIdentities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open seeded legacy database: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET provider = '' WHERE event_key IN (?, ?)", "legacy-apikey", "legacy-oauth").Error; err != nil {
		t.Fatalf("blank legacy usage event providers: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET provider = ? WHERE event_key = ?", "existing-provider", "apikey-success").Error; err != nil {
		t.Fatalf("set existing provider: %v", err)
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var legacyProvider models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-apikey").First(&legacyProvider).Error; err != nil {
		t.Fatalf("load legacy provider event: %v", err)
	}
	if legacyProvider.AuthType != "apikey" || legacyProvider.Provider != "Claude API" {
		t.Fatalf("expected legacy provider event identity fields to be backfilled, got %+v", legacyProvider)
	}

	var legacyOAuth models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-oauth").First(&legacyOAuth).Error; err != nil {
		t.Fatalf("load legacy oauth event: %v", err)
	}
	if legacyOAuth.AuthType != "oauth" {
		t.Fatalf("expected legacy oauth event auth_type to be backfilled, got %+v", legacyOAuth)
	}

	var existingProvider models.UsageEvent
	if err := db.Where("event_key = ?", "apikey-success").First(&existingProvider).Error; err != nil {
		t.Fatalf("load existing provider event: %v", err)
	}
	if existingProvider.Provider != "existing-provider" {
		t.Fatalf("expected existing provider field to remain unchanged, got %+v", existingProvider)
	}

	var providerFilterCount int64
	if err := db.Model(&models.UsageEvent{}).Where("auth_type = ? AND provider = ?", "apikey", "Claude API").Count(&providerFilterCount).Error; err != nil {
		t.Fatalf("count provider-filtered usage events: %v", err)
	}
	if providerFilterCount != 1 {
		t.Fatalf("expected provider filter to match migrated legacy event, got %d", providerFilterCount)
	}
}

func TestOpenDatabaseUsageIdentityMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)
	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var identities []models.UsageIdentity
	if err := db.Order("auth_type asc, identity asc").Find(&identities).Error; err != nil {
		t.Fatalf("load usage identities after reopen: %v", err)
	}
	if len(identities) != 4 {
		t.Fatalf("expected all 4 usage identities after reopen, got %d: %+v", len(identities), identities)
	}
	oauth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-1")
	if oauth.TotalRequests != 3 || oauth.TotalTokens != 90 || oauth.LastAggregatedUsageEventID != 3 {
		t.Fatalf("expected oauth stats not to double-add after reopen, got %+v", oauth)
	}
	provider := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAIProvider, "api-source-1")
	if provider.TotalRequests != 2 || provider.TotalTokens != 39 || provider.LastAggregatedUsageEventID != 5 {
		t.Fatalf("expected provider stats not to double-add after reopen, got %+v", provider)
	}
	deletedOAuth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-deleted")
	if !deletedOAuth.IsDeleted || deletedOAuth.TotalRequests != 1 || deletedOAuth.TotalTokens != 100 || deletedOAuth.LastAggregatedUsageEventID != 6 {
		t.Fatalf("expected deleted oauth stats not to double-add after reopen, got %+v", deletedOAuth)
	}
	deletedProvider := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAIProvider, "api-deleted")
	if !deletedProvider.IsDeleted || deletedProvider.TotalRequests != 1 || deletedProvider.TotalTokens != 100 || deletedProvider.LastAggregatedUsageEventID != 7 {
		t.Fatalf("expected deleted provider stats not to double-add after reopen, got %+v", deletedProvider)
	}

	var duplicateVersions int64
	if err := db.Table("schema_migrations").Select("COUNT(*) - COUNT(DISTINCT version)").Scan(&duplicateVersions).Error; err != nil {
		t.Fatalf("count duplicate schema migration versions: %v", err)
	}
	if duplicateVersions != 0 {
		t.Fatalf("expected no duplicate schema migration versions, got %d", duplicateVersions)
	}
	for _, version := range []string{"20260504_create_usage_identities", "20260504_migrate_usage_identities_metadata", "20260504_backfill_usage_event_identity_fields", "20260504_backfill_usage_identity_stats", "20260504_drop_legacy_metadata_tables", "20260504_drop_legacy_snapshot_run_columns", "20260504_remove_prefix_usage_identities"} {
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil {
			t.Fatalf("count schema migration %s: %v", version, err)
		}
		if count != 1 {
			t.Fatalf("expected schema migration %s to be recorded once, got %d", version, count)
		}
	}
	if db.Migrator().HasTable("auth_files") || db.Migrator().HasTable("provider_metadata") {
		t.Fatal("expected old metadata tables to stay dropped after reopen")
	}
}

func TestOpenDatabaseSkipsUsageIdentityMetadataMigrationWhenLegacyTablesAreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "no-legacy-identities.db")

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasTable(&models.UsageIdentity{}) {
		t.Fatal("expected usage_identities table to exist")
	}
	var count int64
	if err := db.Model(&models.UsageIdentity{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no usage identities without legacy metadata, got %d", count)
	}
	if db.Migrator().HasTable("auth_files") || db.Migrator().HasTable("provider_metadata") {
		t.Fatal("expected legacy metadata tables not to be recreated")
	}
}

func TestOpenDatabaseRemovesPrefixGeneratedUsageIdentities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "prefix-identities.db")
	seedPrefixGeneratedUsageIdentities(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	for _, prefix := range []string{"gemini", "claude", "codex", "vertex", "openai"} {
		var prefixCount int64
		if err := db.Model(&models.UsageIdentity{}).Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, prefix).Count(&prefixCount).Error; err != nil {
			t.Fatalf("count prefix usage identity %q: %v", prefix, err)
		}
		if prefixCount != 0 {
			t.Fatalf("expected fixed prefix usage identity %q to be removed, got %d", prefix, prefixCount)
		}
	}

	var apiKey models.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, "claude-key").First(&apiKey).Error; err != nil {
		t.Fatalf("load real api key identity: %v", err)
	}
	if apiKey.TotalRequests != 1 || apiKey.LastAggregatedUsageEventID != 1 {
		t.Fatalf("expected real api key identity stats to remain, got %+v", apiKey)
	}

	var unusedSiblingKey models.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, "claude-unused-key").First(&unusedSiblingKey).Error; err != nil {
		t.Fatalf("load unused sibling api key identity: %v", err)
	}
	if unusedSiblingKey.Type != "claude" || unusedSiblingKey.Provider != "Claude Team" {
		t.Fatalf("expected unused sibling api key identity to remain unchanged, got %+v", unusedSiblingKey)
	}

	var unusedKey models.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, "gemini-unused-key").First(&unusedKey).Error; err != nil {
		t.Fatalf("load unused real api key identity: %v", err)
	}
	if unusedKey.Type != "gemini" || unusedKey.Provider != "Gemini Team" {
		t.Fatalf("expected unused real api key identity to remain unchanged, got %+v", unusedKey)
	}

	var customPrefix models.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, "https://proxy.internal/v1").First(&customPrefix).Error; err != nil {
		t.Fatalf("load custom prefix-like identity: %v", err)
	}
	if customPrefix.Type != "openai" || customPrefix.Provider != "Custom OpenAI" {
		t.Fatalf("expected non-fixed custom prefix-like identity to remain unchanged, got %+v", customPrefix)
	}
}

func findUsageIdentity(t *testing.T, identities []models.UsageIdentity, authType models.UsageIdentityAuthType, identity string) models.UsageIdentity {
	t.Helper()
	for _, usageIdentity := range identities {
		if usageIdentity.AuthType == authType && usageIdentity.Identity == identity {
			return usageIdentity
		}
	}
	t.Fatalf("usage identity auth_type=%d identity=%q not found in %+v", authType, identity, identities)
	return models.UsageIdentity{}
}

func seedPrefixGeneratedUsageIdentities(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open prefix identity database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		total_requests integer DEFAULT 0,
		success_count integer DEFAULT 0,
		failure_count integer DEFAULT 0,
		input_tokens integer DEFAULT 0,
		output_tokens integer DEFAULT 0,
		reasoning_tokens integer DEFAULT 0,
		cached_tokens integer DEFAULT 0,
		total_tokens integer DEFAULT 0,
		last_aggregated_usage_event_id integer DEFAULT 0,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric DEFAULT false,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_identities table: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`).Error; err != nil {
		t.Fatalf("create usage identity unique index: %v", err)
	}
	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		api_group_key text,
		provider text,
		endpoint text,
		auth_type text,
		request_id text,
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
	)`).Error; err != nil {
		t.Fatalf("create usage_events table: %v", err)
	}

	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	rows := []models.UsageIdentity{
		{Name: "Claude Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude-key", Type: "claude", Provider: "Claude Team", TotalRequests: 1, SuccessCount: 1, TotalTokens: 30, LastAggregatedUsageEventID: 1, CreatedAt: now, UpdatedAt: now},
		{Name: "Claude Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude-unused-key", Type: "claude", Provider: "Claude Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Gemini Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini", Type: "gemini", Provider: "Gemini Team", TotalRequests: 2, SuccessCount: 2, TotalTokens: 40, LastAggregatedUsageEventID: 2, CreatedAt: now, UpdatedAt: now},
		{Name: "Claude Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude", Type: "claude", Provider: "Claude Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Codex Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "codex", Type: "codex", Provider: "Codex Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Vertex Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "vertex", Type: "vertex", Provider: "Vertex Team", CreatedAt: now, UpdatedAt: now},
		{Name: "OpenAI Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "openai", Type: "openai", Provider: "OpenAI Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Gemini Team", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini-unused-key", Type: "gemini", Provider: "Gemini Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Custom OpenAI", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "https://proxy.internal/v1", Type: "openai", Provider: "Custom OpenAI", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, failed, latency_ms, input_tokens, output_tokens, total_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "claude-event", "group", "Claude Team", "/v1/messages", "apikey", "req", "claude-sonnet", now, "claude-key", false, 100, 10, 20, 30, now).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
}

func seedLegacyUsageIdentityTables(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy identity database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE auth_files (
			id integer PRIMARY KEY AUTOINCREMENT,
			auth_index text,
			name text,
			email text,
			type text,
			provider text,
			label text,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		)`,
		`CREATE TABLE provider_metadata (
			id integer PRIMARY KEY AUTOINCREMENT,
			lookup_key text,
			provider_type text,
			display_name text,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		)`,
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY AUTOINCREMENT,
			event_key text,
			api_group_key text,
			provider text,
			endpoint text,
			auth_type text,
			request_id text,
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
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy identity schema with %q: %v", statement, err)
		}
	}

	now := time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC)
	deletedAt := time.Date(2026, 5, 4, 7, 30, 0, 0, time.UTC)
	if err := db.Exec("INSERT INTO auth_files (auth_index, name, email, type, provider, label, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "auth-1", "OAuth Name", "person@example.com", "claude", "claude", "OAuth Label", now, now, nil).Error; err != nil {
		t.Fatalf("seed active auth file: %v", err)
	}
	if err := db.Exec("INSERT INTO auth_files (auth_index, name, email, type, provider, label, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "auth-deleted", "Deleted OAuth", "deleted@example.com", "claude", "claude", "Deleted", now, now, deletedAt).Error; err != nil {
		t.Fatalf("seed deleted auth file: %v", err)
	}
	if err := db.Exec("INSERT INTO provider_metadata (lookup_key, provider_type, display_name, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?)", "api-source-1", "claude", "Claude API", now, now, nil).Error; err != nil {
		t.Fatalf("seed active provider metadata: %v", err)
	}
	if err := db.Exec("INSERT INTO provider_metadata (lookup_key, provider_type, display_name, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?)", "api-deleted", "claude", "Deleted API", now, now, deletedAt).Error; err != nil {
		t.Fatalf("seed deleted provider metadata: %v", err)
	}

	events := []struct {
		eventKey        string
		authType        string
		authIndex       string
		source          string
		failed          bool
		inputTokens     int64
		outputTokens    int64
		reasoningTokens int64
		cachedTokens    int64
		totalTokens     int64
		timestamp       time.Time
	}{
		{eventKey: "oauth-success", authType: "oauth", authIndex: "auth-1", failed: false, inputTokens: 10, outputTokens: 20, reasoningTokens: 3, cachedTokens: 4, totalTokens: 37, timestamp: time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)},
		{eventKey: "legacy-oauth", authIndex: "auth-1", failed: false, inputTokens: 1, outputTokens: 1, reasoningTokens: 1, cachedTokens: 1, totalTokens: 4, timestamp: time.Date(2026, 5, 4, 8, 30, 0, 0, time.UTC)},
		{eventKey: "oauth-failure", authType: "oauth", authIndex: "auth-1", failed: true, inputTokens: 20, outputTokens: 20, reasoningTokens: 7, cachedTokens: 2, totalTokens: 49, timestamp: time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)},
		{eventKey: "apikey-success", authType: "apikey", source: "api-source-1", failed: false, inputTokens: 7, outputTokens: 8, reasoningTokens: 9, cachedTokens: 10, totalTokens: 34, timestamp: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)},
		{eventKey: "legacy-apikey", source: "api-source-1", failed: false, inputTokens: 2, outputTokens: 1, reasoningTokens: 1, cachedTokens: 1, totalTokens: 5, timestamp: time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)},
		{eventKey: "deleted-oauth", authType: "oauth", authIndex: "auth-deleted", failed: false, totalTokens: 100, timestamp: time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)},
		{eventKey: "deleted-api", authType: "apikey", source: "api-deleted", failed: false, totalTokens: 100, timestamp: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)},
	}
	for _, event := range events {
		if err := db.Exec(
			`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, auth_index, failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.eventKey, "group", "claude", "/v1/messages", event.authType, event.eventKey, "claude-sonnet", event.timestamp, event.source, event.authIndex, event.failed, 100, event.inputTokens, event.outputTokens, event.reasoningTokens, event.cachedTokens, event.totalTokens, event.timestamp,
		).Error; err != nil {
			t.Fatalf("seed usage event %s: %v", event.eventKey, err)
		}
	}
}

func seedLegacyRedisUsageTables(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(sqliteDSN(dbPath)), &gorm.Config{})
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
		{hash: "hash-1", rawMessage: `{"provider":" claude ","endpoint":" /v1/messages ","auth_type":" API_KEY ","request_id":" req-from-raw "}`, status: RedisUsageInboxStatusProcessed, usageEventKey: "legacy-canonical-key", processedAt: &now},
		{hash: "hash-2", rawMessage: `{"provider":" fallback-provider ","endpoint":" /fallback ","auth_type":" OAuth ","request_id":" req-fallback "}`, status: RedisUsageInboxStatusProcessed, usageEventKey: "missing-key", processedAt: &now},
		{hash: "hash-3", rawMessage: `{"provider":" overwrite-provider ","endpoint":" /overwrite ","auth_type":" api_key ","request_id":" overwrite-request "}`, status: RedisUsageInboxStatusProcessed, usageEventKey: "existing-key", processedAt: &now},
		{hash: "hash-4", rawMessage: `{"provider":" blank-provider ","endpoint":" /blank ","auth_type":" OAuth ","request_id":" req-blank-fallback "}`, status: RedisUsageInboxStatusProcessed, usageEventKey: "", processedAt: &now},
		{hash: "hash-5", rawMessage: `{"provider":"pending-provider","request_id":"pending-key"}`, status: RedisUsageInboxStatusPending, usageEventKey: "pending-key"},
	}
	for _, inbox := range inboxes {
		if err := db.Exec(
			"INSERT INTO redis_usage_inboxes (queue_key, message_hash, raw_message, status, attempt_count, usage_event_key, popped_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			"queue", inbox.hash, inbox.rawMessage, inbox.status, 0, inbox.usageEventKey, now, inbox.processedAt, now, now,
		).Error; err != nil {
			t.Fatalf("seed legacy redis inbox: %v", err)
		}
	}
}

func openMigratedDatabase(t *testing.T, dbPath string) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	return db
}

func captureRepositoryLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
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
