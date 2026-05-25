package poller_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestRedisInboxWriterSkipsEmptyMessages(t *testing.T) {
	db := openPollerTestDB(t)
	writer := poller.NewRedisInboxWriter(db, cpa.ManagementUsageQueueKey)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceSubscribe, nil, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("expected no inserted rows, got %d", inserted)
	}

	var count int64
	if err := db.Model(&entities.RedisUsageInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count redis inbox rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no redis inbox rows, got %d", count)
	}
}

func TestRedisInboxWriterPersistsMessagesWithSource(t *testing.T) {
	db := openPollerTestDB(t)
	receivedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	writer := poller.NewRedisInboxWriter(db, cpa.ManagementUsageQueueKey)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceSubscribe, []string{`{"request_id":"one"}`}, receivedAt)
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected one inserted row, got %d", inserted)
	}

	var rows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("list redis inbox rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one redis inbox row, got %d", len(rows))
	}
	if rows[0].QueueKey != cpa.ManagementUsageQueueKey {
		t.Fatalf("expected queue key %q, got %q", cpa.ManagementUsageQueueKey, rows[0].QueueKey)
	}
	if rows[0].RawMessage != `{"request_id":"one"}` {
		t.Fatalf("unexpected raw message %q", rows[0].RawMessage)
	}
	if !rows[0].PoppedAt.Equal(receivedAt) {
		t.Fatalf("expected received at %s, got %s", receivedAt, rows[0].PoppedAt)
	}
}

func openPollerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
