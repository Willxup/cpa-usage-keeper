package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseBackfillsUsageEventRedisFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedLegacyRedisUsageTables(t, dbPath)
	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)
	for _, tc := range []struct{ name, eventKey, provider, endpoint, authType, requestID string }{
		{"canonical key", "legacy-canonical-key", "claude", "/v1/messages", "apikey", "req-from-raw"},
		{"request ID fallback", "req-fallback", "fallback-provider", "/fallback", "oauth", "req-fallback"},
		{"blank usage key fallback", "req-blank-fallback", "blank-provider", "/blank", "oauth", "req-blank-fallback"},
		{name: "empty event key remains unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var event entities.UsageEvent
			if err := db.Where("event_key = ?", tc.eventKey).First(&event).Error; err != nil {
				t.Fatalf("load usage event: %v", err)
			}
			if event.Provider != tc.provider || event.Endpoint != tc.endpoint || event.AuthType != tc.authType || event.RequestID != tc.requestID {
				t.Fatalf("unexpected backfill for %q: %+v", tc.eventKey, event)
			}
		})
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

	var event entities.UsageEvent
	if err := db.Where("event_key = ?", "existing-key").First(&event).Error; err != nil {
		t.Fatalf("load existing usage event: %v", err)
	}
	if event.Provider != "existing-provider" || event.Endpoint != "existing-endpoint" || event.AuthType != "existing-auth" || event.RequestID != "existing-request" {
		t.Fatalf("expected existing fields to remain unchanged, got %+v", event)
	}
}
