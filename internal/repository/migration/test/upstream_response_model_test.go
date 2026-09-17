package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const upstreamResponseModelMigrationVersion = "20260917_add_usage_event_upstream_response_model"

func TestUsageEventUpstreamResponseModelMigrationAddsEmptyColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id INTEGER PRIMARY KEY
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_events (id) VALUES (1)").Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", upstreamResponseModelMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make upstream response model migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn("usage_events", "upstream_response_model") {
		t.Fatal("expected usage_events.upstream_response_model column")
	}
	var value string
	if err := db.Raw("SELECT upstream_response_model FROM usage_events WHERE id = 1").Scan(&value).Error; err != nil {
		t.Fatalf("load migrated value: %v", err)
	}
	if value != "" {
		t.Fatalf("got %q, want empty", value)
	}
}
