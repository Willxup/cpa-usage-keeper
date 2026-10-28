package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
)

const effectiveServiceTierMigrationVersion = "20261023_usage_event_effective_service_tier"

func TestEffectiveServiceTierMigrationPreservesHotAndArchiveRows(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, statement := range []string{
			"CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, service_tier TEXT, response_service_tier TEXT)",
			"INSERT INTO " + table + " (id, service_tier, response_service_tier) VALUES (1, 'auto', 'default')",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	runOnlyMigration(t, db, effectiveServiceTierMigrationVersion)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var row struct {
			ServiceTier          string
			EffectiveServiceTier string
			ResponseServiceTier  string
		}
		if err := db.Table(table).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.ServiceTier != "auto" || row.EffectiveServiceTier != "" || row.ResponseServiceTier != "default" {
			t.Fatalf("migration changed historical tier evidence in %s: %+v", table, row)
		}
		if err := db.Table(table).Where("id = 1").Update("effective_service_tier", "CustomTier").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("schema_migrations").Where("version = ?", effectiveServiceTierMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var tier string
		if err := db.Table(table).Where("id = 1").Pluck("effective_service_tier", &tier).Error; err != nil {
			t.Fatal(err)
		}
		if tier != "CustomTier" {
			t.Fatalf("rerun lost recorded tier in %s: %q", table, tier)
		}
	}
}

func TestEffectiveServiceTierMigrationSkipsMissingTables(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	runOnlyMigration(t, db, effectiveServiceTierMigrationVersion)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("migration unexpectedly created %s", table)
		}
	}
}
