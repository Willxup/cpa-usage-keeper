package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventReasoningEffortMigrationAddsDefaultedColumn(t *testing.T) {
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
		VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-05-19 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := addUsageEventReasoningEffortMigration(db); err != nil {
		t.Fatalf("add usage event reasoning effort: %v", err)
	}
	if err := addUsageEventReasoningEffortMigration(db); err != nil {
		t.Fatalf("add usage event reasoning effort should be idempotent: %v", err)
	}

	if !db.Migrator().HasColumn("usage_events", "reasoning_effort") {
		t.Fatal("expected usage_events.reasoning_effort column to exist")
	}

	var reasoningEffort string
	if err := db.Raw(`SELECT reasoning_effort FROM usage_events WHERE id = ?`, int64(1)).Row().Scan(&reasoningEffort); err != nil {
		t.Fatalf("scan reasoning effort: %v", err)
	}
	if reasoningEffort != "" {
		t.Fatalf("expected reasoning effort to default to empty string, got %q", reasoningEffort)
	}
}
