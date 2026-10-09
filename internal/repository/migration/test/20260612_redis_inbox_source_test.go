package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReplaceRedisInboxQueueKeyWithSourceMigrationAddsSourceAndDropsQueueKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE redis_usage_inboxes (
		id integer PRIMARY KEY AUTOINCREMENT,
		queue_key text NOT NULL,
		message_hash text NOT NULL,
		raw_message text NOT NULL,
		status text NOT NULL,
		attempt_count integer NOT NULL DEFAULT 0,
		usage_event_key text,
		popped_at datetime NOT NULL,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy redis_usage_inboxes table: %v", err)
	}
	if err := db.Exec(`CREATE INDEX idx_redis_usage_inboxes_queue_key ON redis_usage_inboxes(queue_key)`).Error; err != nil {
		t.Fatalf("create legacy queue_key index: %v", err)
	}
	if err := db.Exec(`INSERT INTO redis_usage_inboxes (
		queue_key, message_hash, raw_message, status, popped_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"queue", "hash-one", `{"request_id":"one"}`, "pending",
		"2026-06-12T10:00:00+08:00", "2026-06-12T10:00:00+08:00", "2026-06-12T10:00:00+08:00",
	).Error; err != nil {
		t.Fatalf("seed legacy redis_usage_inboxes row: %v", err)
	}

	if err := runLegacyMigration(db, "20260612_replace_redis_inbox_queue_key_with_source"); err != nil {
		t.Fatalf("replaceRedisInboxQueueKeyWithSourceMigration returned error: %v", err)
	}
	if err := runLegacyMigration(db, "20260612_replace_redis_inbox_queue_key_with_source"); err != nil {
		t.Fatalf("replaceRedisInboxQueueKeyWithSourceMigration idempotently returned error: %v", err)
	}

	if db.Migrator().HasColumn("redis_usage_inboxes", "queue_key") {
		t.Fatal("expected redis_usage_inboxes.queue_key column to be removed")
	}
	if sqliteIndexExists(t, db, "idx_redis_usage_inboxes_queue_key") {
		t.Fatal("expected legacy queue_key index to be removed")
	}

	var source string
	if err := db.Table("redis_usage_inboxes").Select("source").Where("id = ?", 1).Scan(&source).Error; err != nil {
		t.Fatalf("load migrated source: %v", err)
	}
	if source != "unknown" {
		t.Fatalf("expected migrated source to be unknown, got %q", source)
	}
}
