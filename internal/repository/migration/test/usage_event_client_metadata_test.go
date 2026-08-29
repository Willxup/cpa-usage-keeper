package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageEventClientMetadataMigrationVersion = "20260729_add_usage_event_client_metadata"

func TestUsageEventClientMetadataMigrationKeepsExistingRowsNull(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (id) VALUES (1)`).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", usageEventClientMetadataMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make client metadata migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for _, column := range []string{"client_ip", "x_forwarded_for", "user_agent"} {
		if !db.Migrator().HasColumn("usage_events", column) {
			t.Fatalf("expected usage_events.%s column", column)
		}
	}

	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM usage_events WHERE id = 1 AND client_ip IS NULL AND x_forwarded_for IS NULL AND user_agent IS NULL`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("check migrated values: %v", err)
	}
	if nullCount != 1 {
		t.Fatalf("expected existing usage event metadata to remain NULL, got count %d", nullCount)
	}
}
