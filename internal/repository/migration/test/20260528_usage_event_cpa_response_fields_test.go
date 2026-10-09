package test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventCPAResponseFieldsMigrationAddsColumns(t *testing.T) {
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
		VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-05-28 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := runLegacyMigration(db, "20260528_add_usage_event_cpa_response_fields"); err != nil {
		t.Fatalf("add usage event CPA response fields: %v", err)
	}
	if err := runLegacyMigration(db, "20260528_add_usage_event_cpa_response_fields"); err != nil {
		t.Fatalf("add usage event CPA response fields should be idempotent: %v", err)
	}

	var ttftMS sql.NullInt64
	var serviceTier string
	if err := db.Raw(`SELECT ttft_ms, service_tier FROM usage_events WHERE id = ?`, int64(1)).Row().Scan(&ttftMS, &serviceTier); err != nil {
		t.Fatalf("scan CPA response fields: %v", err)
	}
	if ttftMS.Valid {
		t.Fatalf("expected existing usage event ttft_ms to stay NULL, got %d", ttftMS.Int64)
	}
	if serviceTier != "" {
		t.Fatalf("expected existing usage event service_tier to default to empty string, got %q", serviceTier)
	}
}
