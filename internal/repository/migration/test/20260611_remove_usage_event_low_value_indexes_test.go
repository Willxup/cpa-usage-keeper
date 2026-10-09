package test

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemoveUsageEventLowValueIndexesMigrationDropsUpgradeOnlyIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		api_group_key text,
		auth_type text,
		auth_index text,
		model text,
		provider text,
		source text,
		timestamp datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_events table: %v", err)
	}
	for _, statement := range []string{
		`CREATE INDEX idx_usage_events_timestamp_id ON usage_events(timestamp DESC, id DESC)`,
		`CREATE INDEX idx_usage_events_api_group_key ON usage_events(api_group_key)`,
		`CREATE INDEX idx_usage_events_auth_index ON usage_events(auth_index)`,
		`CREATE INDEX idx_usage_events_model ON usage_events(model)`,
		`CREATE INDEX idx_usage_events_auth_type_auth_index_id ON usage_events(auth_type, auth_index, id)`,
		`CREATE INDEX idx_usage_events_source ON usage_events(source)`,
		`CREATE INDEX idx_usage_events_provider ON usage_events(provider)`,
		`CREATE INDEX idx_usage_events_auth_type ON usage_events(auth_type)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed usage_events index: %v", err)
		}
	}

	if err := runLegacyMigration(db, "20260611_remove_usage_event_low_value_indexes"); err != nil {
		t.Fatalf("removeUsageEventLowValueIndexesMigration returned error: %v", err)
	}

	for _, indexName := range []string{
		"idx_usage_events_source",
		"idx_usage_events_provider",
		"idx_usage_events_auth_type",
	} {
		if sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected low-value index %s to be dropped", indexName)
		}
	}
	for _, indexName := range []string{
		"idx_usage_events_timestamp_id",
		"idx_usage_events_api_group_key",
		"idx_usage_events_auth_index",
		"idx_usage_events_model",
		"idx_usage_events_auth_type_auth_index_id",
	} {
		if !sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected retained index %s to remain", indexName)
		}
	}
}
