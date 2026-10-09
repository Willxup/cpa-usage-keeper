package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventExecutorTypeMigrationAddsColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY,
		event_key text,
		model text,
		timestamp datetime,
		source text,
		auth_index text,
		total_tokens integer
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (id, event_key, model, timestamp, source, auth_index, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-06-02 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := runLegacyMigration(db, "20260602_add_usage_event_executor_type"); err != nil {
		t.Fatalf("add usage event executor_type: %v", err)
	}
	if err := runLegacyMigration(db, "20260602_add_usage_event_executor_type"); err != nil {
		t.Fatalf("add usage event executor_type should be idempotent: %v", err)
	}

	var executorType string
	if err := db.Raw(`SELECT executor_type FROM usage_events WHERE id = ?`, int64(1)).Row().Scan(&executorType); err != nil {
		t.Fatalf("scan executor_type: %v", err)
	}
	if executorType != "" {
		t.Fatalf("expected existing usage event executor_type to default to empty string, got %q", executorType)
	}
}
