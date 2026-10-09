package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageEventResponseModelMigrationVersion = "20260918_usage_event_response_model"

func TestUsageEventResponseModelMigrationAddsHotAndArchiveColumnsWithoutChangingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, model TEXT)").Error; err != nil {
			t.Fatalf("create legacy %s table: %v", table, err)
		}
		if err := db.Exec("INSERT INTO " + table + " (id, model) VALUES (1, 'gpt-6-astra')").Error; err != nil {
			t.Fatalf("seed legacy %s row: %v", table, err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", usageEventResponseModelMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make response model migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if !db.Migrator().HasColumn(table, "response_model") {
			t.Fatalf("expected %s.response_model column", table)
		}
		var model, responseModel string
		if err := db.Raw("SELECT model, response_model FROM "+table+" WHERE id = 1").Row().Scan(&model, &responseModel); err != nil {
			t.Fatalf("read migrated %s row: %v", table, err)
		}
		if model != "gpt-6-astra" || responseModel != "" {
			t.Fatalf("expected %s row preserved with empty response model, model=%q response_model=%q", table, model, responseModel)
		}
	}
}
