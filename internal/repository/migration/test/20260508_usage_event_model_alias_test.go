package test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventModelAliasMigrationAddsNullableColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		model text,
		timestamp datetime,
		source text,
		auth_index text,
		total_tokens integer
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (event_key, model, timestamp, source, auth_index, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?)`, "event-1", "claude-sonnet", "2026-05-07 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := runLegacyMigration(db, "20260508_add_usage_event_model_alias"); err != nil {
		t.Fatalf("add usage event model alias: %v", err)
	}

	if !db.Migrator().HasColumn("usage_events", "model_alias") {
		t.Fatal("expected usage_events.model_alias column to exist")
	}

	var modelAlias sql.NullString
	if err := db.Raw(`SELECT model_alias FROM usage_events WHERE event_key = ?`, "event-1").Row().Scan(&modelAlias); err != nil {
		t.Fatalf("scan model alias: %v", err)
	}
	if modelAlias.Valid {
		t.Fatalf("expected usage_events.model_alias to default NULL, got %+v", modelAlias)
	}
}
