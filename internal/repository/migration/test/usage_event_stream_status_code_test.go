package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUsageEventStreamStatusCodeMigrationAddsHotAndArchiveColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, event_key TEXT NOT NULL)").Error; err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", "20260919_usage_event_stream_status_code").Delete(nil).Error; err != nil {
		t.Fatalf("make migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, column := range []string{"status_code", "stream"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("expected %s.%s column", table, column)
			}
		}
	}
}
