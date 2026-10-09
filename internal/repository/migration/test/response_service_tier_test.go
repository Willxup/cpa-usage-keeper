package test

import "testing"

const responseServiceTierMigrationVersion = "20260715_add_usage_event_response_service_tier"

func TestUsageEventResponseServiceTierMigrationBackfillsEveryExistingRow(t *testing.T) {
	db := openUnmigratedTestDatabase(t)

	if err := db.Exec(`CREATE TABLE usage_events (
		id INTEGER PRIMARY KEY,
		service_tier TEXT NOT NULL DEFAULT ''
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	for id, tier := range []string{"auto", "default", "priority", "fast", "flex", "", " AUTO ", "custom"} {
		if err := db.Exec("INSERT INTO usage_events (id, service_tier) VALUES (?, ?)", id+1, tier).Error; err != nil {
			t.Fatalf("seed usage event %d: %v", id+1, err)
		}
	}
	runOnlyMigration(t, db, responseServiceTierMigrationVersion)

	if !db.Migrator().HasColumn("usage_events", "response_service_tier") {
		t.Fatal("expected usage_events.response_service_tier column")
	}
	type tierRow struct {
		ID                  int64
		ServiceTier         string
		ResponseServiceTier string
	}
	var rows []tierRow
	if err := db.Table("usage_events").Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load migrated usage events: %v", err)
	}
	want := []string{"default", "default", "priority", "fast", "flex", "", "default", "custom"}
	if len(rows) != len(want) {
		t.Fatalf("expected %d migrated rows, got %d", len(want), len(rows))
	}
	for index, row := range rows {
		if row.ResponseServiceTier != want[index] {
			t.Errorf("row %d service_tier=%q response_service_tier=%q, want %q", row.ID, row.ServiceTier, row.ResponseServiceTier, want[index])
		}
	}

	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", responseServiceTierMigrationVersion).Count(&migrationCount).Error; err != nil {
		t.Fatalf("count response service tier migration record: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one response service tier migration record, got %d", migrationCount)
	}
}
