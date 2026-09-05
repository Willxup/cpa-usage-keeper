package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	usageEventAPIGroupKeyTimestampIndexName = "idx_usage_events_api_group_key_timestamp"
	usageEventAPIGroupKeyTimestampMigration = "20260905_usage_event_api_group_key_timestamp_index"
)

func TestUsageEventAPIGroupKeyTimestampIndexMigration(t *testing.T) {
	for _, test := range []struct {
		name      string
		seedIndex bool
	}{
		{name: "missing index", seedIndex: false},
		{name: "existing index", seedIndex: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
			if err != nil {
				t.Fatalf("open existing database: %v", err)
			}
			closeMigrationTestDatabase(t, db)

			if err := db.Exec(`CREATE TABLE usage_events (
				id INTEGER PRIMARY KEY,
				api_group_key TEXT NOT NULL,
				timestamp DATETIME NOT NULL
			)`).Error; err != nil {
				t.Fatalf("create usage_events table: %v", err)
			}
			if test.seedIndex {
				if err := db.Exec(`CREATE INDEX idx_usage_events_api_group_key_timestamp
					ON usage_events(api_group_key, timestamp DESC)`).Error; err != nil {
					t.Fatalf("seed index: %v", err)
				}
			}
			if err := db.Exec(`INSERT INTO usage_events (id, api_group_key, timestamp)
				VALUES (1, 'default-key', '2026-09-05T00:00:00Z')`).Error; err != nil {
				t.Fatalf("seed usage event: %v", err)
			}
			if err := migration.MarkAllAsApplied(db); err != nil {
				t.Fatalf("mark historical migrations applied: %v", err)
			}
			if err := db.Table("schema_migrations").Where("version = ?", usageEventAPIGroupKeyTimestampMigration).Delete(nil).Error; err != nil {
				t.Fatalf("make index migration pending: %v", err)
			}

			if err := migration.Run(db); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}

			if !db.Migrator().HasIndex("usage_events", usageEventAPIGroupKeyTimestampIndexName) {
				t.Fatalf("expected index %s to exist", usageEventAPIGroupKeyTimestampIndexName)
			}
			var eventIDs []int64
			if err := db.Raw(`SELECT id
				FROM usage_events
				WHERE api_group_key = ? AND timestamp >= ? AND timestamp < ?
				ORDER BY timestamp DESC`,
				"default-key", "2026-09-01T00:00:00Z", "2026-09-06T00:00:00Z").Scan(&eventIDs).Error; err != nil {
				t.Fatalf("query usage events through index: %v", err)
			}
			if len(eventIDs) != 1 || eventIDs[0] != 1 {
				t.Fatalf("expected query to return event 1, got %v", eventIDs)
			}
		})
	}
}
