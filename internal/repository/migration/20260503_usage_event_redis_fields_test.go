package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByUsageEventKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-canonical-key").First(&event).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "req-from-raw" {
		t.Fatalf("expected backfill by usage_event_key, got %+v", event)
	}
}

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByRawRequestIDFallback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "req-fallback").First(&event).Error; err != nil {
		t.Fatalf("load fallback usage event: %v", err)
	}
	if event.Provider != "fallback-provider" || event.Endpoint != "/fallback" || event.AuthType != "oauth" || event.RequestID != "req-fallback" {
		t.Fatalf("expected fallback backfill by raw request_id, got %+v", event)
	}
}

func TestOpenDatabaseBackfillsUsageEventRedisFieldsByRawRequestIDWhenUsageEventKeyIsBlank(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "req-blank-fallback").First(&event).Error; err != nil {
		t.Fatalf("load blank fallback usage event: %v", err)
	}
	if event.Provider != "blank-provider" || event.Endpoint != "/blank" || event.AuthType != "oauth" || event.RequestID != "req-blank-fallback" {
		t.Fatalf("expected blank usage_event_key to fall back by raw request_id, got %+v", event)
	}

	var emptyEvent models.UsageEvent
	if err := db.Where("event_key = ?", "").First(&emptyEvent).Error; err != nil {
		t.Fatalf("load empty-key usage event: %v", err)
	}
	if emptyEvent.Provider != "" || emptyEvent.Endpoint != "" || emptyEvent.AuthType != "" || emptyEvent.RequestID != "" {
		t.Fatalf("expected empty-key usage event to remain unchanged, got %+v", emptyEvent)
	}
}

func TestOpenDatabaseBackfillDoesNotOverwriteExistingUsageEventFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)

	// 模拟目标列已经有值的部分迁移数据库。
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open partially migrated database: %v", err)
	}
	for _, statement := range []string{
		"ALTER TABLE usage_events ADD COLUMN provider TEXT",
		"ALTER TABLE usage_events ADD COLUMN endpoint TEXT",
		"ALTER TABLE usage_events ADD COLUMN auth_type TEXT",
		"ALTER TABLE usage_events ADD COLUMN request_id TEXT",
		"UPDATE usage_events SET provider = 'existing-provider', endpoint = 'existing-endpoint', auth_type = 'existing-auth', request_id = 'existing-request' WHERE event_key = 'existing-key'",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare partially migrated database with %q: %v", statement, err)
		}
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var event models.UsageEvent
	if err := db.Where("event_key = ?", "existing-key").First(&event).Error; err != nil {
		t.Fatalf("load existing usage event: %v", err)
	}
	if event.Provider != "existing-provider" || event.Endpoint != "existing-endpoint" || event.AuthType != "existing-auth" || event.RequestID != "existing-request" {
		t.Fatalf("expected existing fields to remain unchanged, got %+v", event)
	}
}
