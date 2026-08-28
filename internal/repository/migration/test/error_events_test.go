package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const errorEventsMigrationVersion = "20260820_create_error_events"

func TestErrorEventsMigrationCreatesEveryFlattenedColumnAndQueryIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-error-events.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	if err := db.Exec("CREATE TABLE legacy_sentinel (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create legacy sentinel: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", errorEventsMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make CPA error events migration pending: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !db.Migrator().HasTable("error_events") {
		t.Fatal("expected error_events table")
	}
	if db.Migrator().HasTable("cpa_error_events") {
		t.Fatal("unexpected legacy cpa_error_events table")
	}

	for _, column := range []string{
		"id", "timestamp", "received_at", "provider", "model", "auth_id", "auth_index", "status_code", "body", "code", "retryable",
		"auth_status", "auth_status_message", "auth_disabled", "auth_unavailable", "auth_next_retry_after",
		"auth_quota_exceeded", "auth_quota_reason", "auth_quota_next_recover_at", "auth_quota_backoff_level",
		"auth_model_name", "auth_model_status", "auth_model_status_message", "auth_model_unavailable", "auth_model_next_retry_after",
		"auth_model_quota_exceeded", "auth_model_quota_reason", "auth_model_quota_next_recover_at", "auth_model_quota_backoff_level",
	} {
		if !db.Migrator().HasColumn(&entities.ErrorEvent{}, column) {
			t.Fatalf("expected error_events.%s column", column)
		}
	}
	if !db.Migrator().HasIndex(&entities.ErrorEvent{}, "idx_error_events_auth_index_timestamp_id") {
		t.Fatal("expected auth_index/timestamp/id query index")
	}
}
