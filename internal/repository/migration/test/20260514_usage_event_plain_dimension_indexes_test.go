package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageEventPlainDimensionIndexesMigrationCreatesIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "usage-event-plain-indexes.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (id integer primary key, api_group_key text, model text, provider text, auth_type text, auth_index text, source text)`).Error; err != nil {
		t.Fatalf("create usage_events: %v", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX idx_usage_events_trim_model ON usage_events(TRIM(model))`,
		`CREATE INDEX idx_usage_events_trim_source ON usage_events(TRIM(source))`,
		`CREATE INDEX idx_usage_events_trim_auth_index ON usage_events(TRIM(auth_index))`,
		`CREATE INDEX idx_usage_events_trim_provider ON usage_events(TRIM(provider))`,
		`CREATE INDEX idx_usage_events_trim_auth_type ON usage_events(TRIM(auth_type))`,
		`CREATE INDEX idx_usage_events_trim_api_group_key ON usage_events(TRIM(api_group_key))`,
		`CREATE INDEX idx_usage_events_auth_type_source_id ON usage_events(auth_type, source, id)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed obsolete usage_events index: %v", err)
		}
	}

	if err := runLegacyMigration(db, "20260514_add_usage_event_plain_dimension_indexes"); err != nil {
		t.Fatalf("addUsageEventPlainDimensionIndexesMigration returned error: %v", err)
	}
	if err := runLegacyMigration(db, "20260514_add_usage_event_plain_dimension_indexes"); err != nil {
		t.Fatalf("addUsageEventPlainDimensionIndexesMigration should be idempotent: %v", err)
	}

	for _, indexName := range []string{
		"idx_usage_events_api_group_key",
		"idx_usage_events_auth_index",
		"idx_usage_events_model",
	} {
		if !sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected index %s to exist", indexName)
		}
	}
	for _, indexName := range []string{
		"idx_usage_events_source",
		"idx_usage_events_trim_model",
		"idx_usage_events_trim_source",
		"idx_usage_events_trim_auth_index",
		"idx_usage_events_trim_provider",
		"idx_usage_events_trim_auth_type",
		"idx_usage_events_trim_api_group_key",
		"idx_usage_events_auth_type_source_id",
	} {
		if sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected obsolete index %s not to exist", indexName)
		}
	}
}
