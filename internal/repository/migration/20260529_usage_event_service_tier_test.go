package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventServiceTierMigrationAddsDefaultedColumn(t *testing.T) {
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
		VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-05-29 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := addUsageEventServiceTierMigration(db); err != nil {
		t.Fatalf("add usage event service tier: %v", err)
	}
	if err := addUsageEventServiceTierMigration(db); err != nil {
		t.Fatalf("add usage event service tier should be idempotent: %v", err)
	}

	if !db.Migrator().HasColumn("usage_events", "service_tier") {
		t.Fatal("expected usage_events.service_tier column to exist")
	}

	var serviceTier string
	if err := db.Raw(`SELECT service_tier FROM usage_events WHERE id = ?`, int64(1)).Row().Scan(&serviceTier); err != nil {
		t.Fatalf("scan service_tier: %v", err)
	}
	if serviceTier != "" {
		t.Fatalf("expected existing usage event service_tier to default to empty string, got %q", serviceTier)
	}
}
