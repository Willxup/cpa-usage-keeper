package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageEventGenerateMigrationVersion = "20260715_add_usage_event_generate"

func TestUsageEventGenerateMigrationBackfillsLegacyPrewarmEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id INTEGER PRIMARY KEY,
		failed NUMERIC NOT NULL DEFAULT 0,
		executor_type TEXT NOT NULL DEFAULT '',
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		cache_read_tokens INTEGER NOT NULL DEFAULT 0,
		cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_events table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (
		id, failed, executor_type, input_tokens, output_tokens, reasoning_tokens,
		cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens
	) VALUES
		(1, 0, 'CodexWebsocketsExecutor', 0, 0, 0, 0, 0, 0, 0),
		(2, 0, 'CodexWebsocketsExecutor', 0, 0, 0, 0, 0, 1, 0),
		(3, 1, 'CodexWebsocketsExecutor', 0, 0, 0, 0, 0, 0, 0),
		(4, 0, 'CodexExecutor',           0, 0, 0, 0, 0, 0, 0),
		(5, 0, 'CodexWebsocketsExecutor', 1, 0, 0, 0, 0, 0, 1)
	`).Error; err != nil {
		t.Fatalf("seed legacy usage events: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", usageEventGenerateMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make generate migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if !db.Migrator().HasColumn("usage_events", "generate") {
		t.Fatal("expected usage_events.generate column")
	}
	type generateRow struct {
		ID       int64
		Generate bool
	}
	var rows []generateRow
	if err := db.Table("usage_events").Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load migrated usage events: %v", err)
	}
	want := []bool{false, true, true, true, true}
	if len(rows) != len(want) {
		t.Fatalf("expected %d migrated rows, got %d", len(want), len(rows))
	}
	for index, row := range rows {
		if row.Generate != want[index] {
			t.Errorf("row %d generate=%v, want %v", row.ID, row.Generate, want[index])
		}
	}

	if err := db.Exec("INSERT INTO usage_events (id) VALUES (?)", 6).Error; err != nil {
		t.Fatalf("insert usage event with default generate: %v", err)
	}
	var defaultGenerate bool
	if err := db.Table("usage_events").Select("generate").Where("id = ?", 6).Scan(&defaultGenerate).Error; err != nil {
		t.Fatalf("load default generate: %v", err)
	}
	if !defaultGenerate {
		t.Fatal("expected usage_events.generate to default true")
	}
	if err := db.Exec("INSERT INTO usage_events (id, generate) VALUES (?, NULL)", 7).Error; err == nil {
		t.Fatal("expected usage_events.generate to reject NULL")
	}

	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", usageEventGenerateMigrationVersion).Count(&migrationCount).Error; err != nil {
		t.Fatalf("count generate migration record: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one generate migration record, got %d", migrationCount)
	}
}
