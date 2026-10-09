package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemoveUsageEventEventKeyUniqueIndexMigrationAllowsDuplicateEventKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "usage-event-event-key-index.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (id integer primary key, event_key text, model text)`).Error; err != nil {
		t.Fatalf("create usage_events: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_usage_events_event_key ON usage_events(event_key)`).Error; err != nil {
		t.Fatalf("create legacy unique index: %v", err)
	}

	if err := runLegacyMigration(db, "20260514_remove_usage_event_event_key_unique_index"); err != nil {
		t.Fatalf("removeUsageEventEventKeyUniqueIndexMigration returned error: %v", err)
	}

	if err := db.Exec(`INSERT INTO usage_events (event_key, model) VALUES (?, ?), (?, ?)`, "request-1", "model-a", "request-1", "model-b").Error; err != nil {
		t.Fatalf("expected duplicate event_key rows to insert after migration: %v", err)
	}
}
