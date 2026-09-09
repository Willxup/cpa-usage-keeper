package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	usageEventQuotaWindowIndexName            = "idx_usage_events_auth_index_timestamp_id"
	usageEventQuotaWindowIndexRepairMigration = "20260902_repair_usage_event_quota_window_index"
)

func TestUsageEventQuotaWindowIndexRepairMigrationReconcilesPhysicalIndex(t *testing.T) {
	for _, test := range []struct {
		name         string
		seedIndex    bool
		indexOutcome string
	}{
		{name: "missing index", indexOutcome: "repaired"},
		{name: "existing index", seedIndex: true, indexOutcome: "preserved"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
			if err != nil {
				t.Fatalf("open existing database: %v", err)
			}
			closeMigrationTestDatabase(t, db)

			if err := db.Exec(`CREATE TABLE usage_events (
				id INTEGER PRIMARY KEY,
				auth_index TEXT NOT NULL,
				timestamp DATETIME NOT NULL
			)`).Error; err != nil {
				t.Fatalf("create usage_events table: %v", err)
			}
			if test.seedIndex {
				if err := db.Exec(`CREATE INDEX idx_usage_events_auth_index_timestamp_id
					ON usage_events(auth_index, timestamp, id)`).Error; err != nil {
					t.Fatalf("seed quota window index: %v", err)
				}
			}
			if err := db.Exec(`INSERT INTO usage_events (id, auth_index, timestamp)
				VALUES (1, 'codex-auth', '2026-09-02T00:00:00Z')`).Error; err != nil {
				t.Fatalf("seed usage event: %v", err)
			}
			// 模拟生产事故：旧建索引 migration 已记录完成，新修复版本仍待执行。
			if err := migration.MarkAllAsApplied(db); err != nil {
				t.Fatalf("mark historical migrations applied: %v", err)
			}
			if err := db.Table("schema_migrations").Where("version = ?", usageEventQuotaWindowIndexRepairMigration).Delete(nil).Error; err != nil {
				t.Fatalf("make quota window index repair migration pending: %v", err)
			}

			if err := migration.Run(db); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}

			if !db.Migrator().HasIndex("usage_events", usageEventQuotaWindowIndexName) {
				t.Fatalf("expected %s index %s", test.indexOutcome, usageEventQuotaWindowIndexName)
			}
			var eventIDs []int64
			if err := db.Raw(`SELECT id
				FROM usage_events INDEXED BY idx_usage_events_auth_index_timestamp_id
				WHERE auth_index = ? AND timestamp >= ? AND timestamp < ?
				ORDER BY timestamp ASC, id ASC`,
				"codex-auth", "2026-09-01T00:00:00Z", "2026-09-03T00:00:00Z").Scan(&eventIDs).Error; err != nil {
				t.Fatalf("query usage events through repaired quota window index: %v", err)
			}
			if len(eventIDs) != 1 || eventIDs[0] != 1 {
				t.Fatalf("expected repaired quota window query to return event 1, got %v", eventIDs)
			}
			var count int64
			if err := db.Table("usage_events").Where("id = ? AND auth_index = ?", 1, "codex-auth").Count(&count).Error; err != nil {
				t.Fatalf("count preserved usage event: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected migration to preserve usage event, got %d rows", count)
			}
			if err := db.Table("schema_migrations").Where("version = ?", usageEventQuotaWindowIndexRepairMigration).Count(&count).Error; err != nil {
				t.Fatalf("count quota window index repair migration: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected one quota window index repair migration record, got %d", count)
			}
		})
	}
}
