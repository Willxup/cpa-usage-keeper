package migration

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/models"
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
	db := openMigratedDatabase(t, filepath.Join(t.TempDir(), "app.db"))
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasTable("schema_migrations") {
		t.Fatal("expected schema_migrations table to exist")
	}
	for _, column := range []string{"provider", "endpoint", "auth_type", "request_id"} {
		if !db.Migrator().HasColumn(&models.UsageEvent{}, column) {
			t.Fatalf("expected usage_events.%s column to exist", column)
		}
	}
	if !db.Migrator().HasColumn(&models.UsageIdentity{}, "lookup_key") {
		t.Fatal("expected usage_identities.lookup_key column to exist")
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

	db := openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != 12 {
		t.Fatalf("expected 12 applied migrations after reopening database, got %d", count)
	}
}

func TestOpenDatabaseLogsSchemaMigrations(t *testing.T) {
	logs := captureMigrationLogs(t, logrus.InfoLevel)
	dbPath := filepath.Join(t.TempDir(), "app.db")

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
