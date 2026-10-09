package test

import (
	"testing"
)

const usageEventClientMetadataMigrationVersion = "20260729_add_usage_event_client_metadata"

func TestUsageEventClientMetadataMigrationKeepsExistingRowsNull(t *testing.T) {
	db := openUnmigratedTestDatabase(t)

	if err := db.Exec(`CREATE TABLE usage_events (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (id) VALUES (1)`).Error; err != nil {
		t.Fatalf("seed legacy usage event: %v", err)
	}
	runOnlyMigration(t, db, usageEventClientMetadataMigrationVersion)

	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM usage_events WHERE id = 1 AND client_ip IS NULL AND x_forwarded_for IS NULL AND user_agent IS NULL`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("check migrated values: %v", err)
	}
	if nullCount != 1 {
		t.Fatalf("expected existing usage event metadata to remain NULL, got count %d", nullCount)
	}
}
