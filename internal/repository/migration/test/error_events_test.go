package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

const errorEventsMigrationVersion = "20260820_create_error_events"

func TestErrorEventsMigrationCreatesEveryFlattenedColumnAndQueryIndex(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	if err := db.Exec("CREATE TABLE legacy_sentinel (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create legacy sentinel: %v", err)
	}
	runOnlyMigration(t, db, errorEventsMigrationVersion)
	if !db.Migrator().HasTable("error_events") {
		t.Fatal("expected error_events table")
	}
	if db.Migrator().HasTable("cpa_error_events") {
		t.Fatal("unexpected legacy cpa_error_events table")
	}

	columnTypes, err := db.Migrator().ColumnTypes(&entities.ErrorEvent{})
	if err != nil {
		t.Fatalf("load error event columns: %v", err)
	}
	columns := make(map[string]bool, len(columnTypes))
	for _, column := range columnTypes {
		columns[column.Name()] = true
	}
	for _, column := range []string{
		"id", "timestamp", "received_at", "provider", "model", "auth_id", "auth_index", "status_code", "body", "code", "retryable",
		"auth_status", "auth_status_message", "auth_disabled", "auth_unavailable", "auth_next_retry_after",
		"auth_quota_exceeded", "auth_quota_reason", "auth_quota_next_recover_at", "auth_quota_backoff_level",
		"auth_model_name", "auth_model_status", "auth_model_status_message", "auth_model_unavailable", "auth_model_next_retry_after",
		"auth_model_quota_exceeded", "auth_model_quota_reason", "auth_model_quota_next_recover_at", "auth_model_quota_backoff_level",
	} {
		if !columns[column] {
			t.Fatalf("expected error_events.%s column", column)
		}
	}
	if !db.Migrator().HasIndex(&entities.ErrorEvent{}, "idx_error_events_auth_index_timestamp_id") {
		t.Fatal("expected auth_index/timestamp/id query index")
	}
}
