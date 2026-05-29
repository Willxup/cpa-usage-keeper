package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventTTFTServiceTierMigrationAddsDefaultedColumns(t *testing.T) {
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
		failed integer,
		latency_ms integer,
		total_tokens integer
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (id, event_key, model, timestamp, source, auth_index, failed, latency_ms, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-05-29 08:00:00", "source-a", "auth-1", 1, 1200, 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := addUsageEventTTFTServiceTierMigration(db); err != nil {
		t.Fatalf("add usage event ttft/service tier: %v", err)
	}
	if err := addUsageEventTTFTServiceTierMigration(db); err != nil {
		t.Fatalf("add usage event ttft/service tier should be idempotent: %v", err)
	}

	for _, column := range []string{"ttft_ms", "service_tier", "fail_status_code"} {
		if !db.Migrator().HasColumn("usage_events", column) {
			t.Fatalf("expected usage_events.%s column to exist", column)
		}
	}

	var (
		ttftMS         int64
		serviceTier    string
		failStatusCode int
	)
	if err := db.Raw(`SELECT ttft_ms, service_tier, fail_status_code FROM usage_events WHERE id = ?`, int64(1)).
		Row().Scan(&ttftMS, &serviceTier, &failStatusCode); err != nil {
		t.Fatalf("scan migrated columns: %v", err)
	}
	if ttftMS != 0 {
		t.Fatalf("expected ttft_ms to default to 0, got %d", ttftMS)
	}
	if serviceTier != "" {
		t.Fatalf("expected service_tier to default to empty string, got %q", serviceTier)
	}
	if failStatusCode != 0 {
		t.Fatalf("expected fail_status_code to default to 0, got %d", failStatusCode)
	}
}
