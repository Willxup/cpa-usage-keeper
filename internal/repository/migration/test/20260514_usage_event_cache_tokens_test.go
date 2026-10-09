package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventCacheTokenFieldsMigrationAddsDefaultedColumns(t *testing.T) {
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
		VALUES (?, ?, ?, ?, ?, ?, ?)`, int64(1), "event-1", "claude-sonnet", "2026-05-14 08:00:00", "source-a", "auth-1", 10).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}

	if err := runLegacyMigration(db, "20260514_add_usage_event_cache_token_fields"); err != nil {
		t.Fatalf("add usage event cache token fields: %v", err)
	}
	if err := runLegacyMigration(db, "20260514_add_usage_event_cache_token_fields"); err != nil {
		t.Fatalf("add usage event cache token fields should be idempotent: %v", err)
	}

	for _, column := range []string{"cache_read_tokens", "cache_creation_tokens"} {
		if !db.Migrator().HasColumn("usage_events", column) {
			t.Fatalf("expected usage_events.%s column to exist", column)
		}
	}

	var cacheReadTokens, cacheCreationTokens int64
	if err := db.Raw(`SELECT cache_read_tokens, cache_creation_tokens FROM usage_events WHERE id = ?`, int64(1)).Row().Scan(&cacheReadTokens, &cacheCreationTokens); err != nil {
		t.Fatalf("scan cache token fields: %v", err)
	}
	if cacheReadTokens != 0 || cacheCreationTokens != 0 {
		t.Fatalf("expected cache token fields to default to zero, got read=%d creation=%d", cacheReadTokens, cacheCreationTokens)
	}
}
