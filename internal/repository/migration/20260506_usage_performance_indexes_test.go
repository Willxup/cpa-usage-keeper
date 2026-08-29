package migration

import (
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseAddsUsagePerformanceIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedPerformanceIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	for _, indexName := range []string{
		"idx_usage_events_timestamp_id",
		"idx_usage_events_api_group_key",
		"idx_usage_events_auth_index",
		"idx_usage_events_model",
		"idx_usage_events_auth_index_timestamp_id",
		"idx_usage_events_auth_type_auth_index_id",
		"idx_redis_usage_inboxes_status_id",
		"idx_redis_usage_inboxes_status_processed_at",
		"idx_redis_usage_inboxes_status_updated_at",
		"idx_redis_usage_inboxes_status_usage_event_key",
		"idx_usage_identities_auth_type_name_id",
		"idx_usage_identities_auth_type_type",
	} {
		if !sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected index %s to exist", indexName)
		}
	}

	for _, indexName := range []string{
		"idx_usage_events_timestamp",
		"idx_usage_events_trim_model",
		"idx_usage_events_trim_source",
		"idx_usage_events_trim_auth_index",
		"idx_usage_events_trim_provider",
		"idx_usage_events_trim_auth_type",
		"idx_usage_events_trim_api_group_key",
		"idx_usage_events_source",
		"idx_usage_events_provider",
		"idx_usage_events_auth_type",
		"idx_usage_events_auth_type_source_id",
		"idx_usage_events_api_group_key_timestamp_id",
		"idx_usage_events_event_key",
		"idx_usage_events_failed",
		"idx_redis_usage_inboxes_status",
		"idx_redis_usage_inboxes_queue_key",
		"idx_redis_usage_inboxes_message_hash",
		"idx_redis_usage_inboxes_usage_event_key",
		"idx_redis_usage_inboxes_popped_at",
		"idx_usage_identities_auth_type",
		"idx_usage_identities_auth_type_name",
		"idx_usage_identities_identity",
		"idx_usage_identities_is_deleted",
		"idx_usage_identities_last_aggregated_usage_event_id",
		"idx_usage_identities_deleted_at",
	} {
		if sqliteIndexExists(t, db, indexName) {
			t.Fatalf("expected redundant index %s to be removed", indexName)
		}
	}

	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260506_add_usage_performance_indexes").Count(&count).Error; err != nil {
		t.Fatalf("count performance index migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected performance index migration to be recorded once, got %d", count)
	}
}

func TestUsagePerformanceIndexMigrationFailsWhenRequiredSchemaIsMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open incomplete legacy database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		timestamp datetime
	)`).Error; err != nil {
		t.Fatalf("create incomplete usage_events table: %v", err)
	}

	err = Run(db)
	if err == nil {
		t.Fatal("expected migration to fail when required schema is missing")
	}
	if !strings.Contains(err.Error(), "missing required schema for performance index migration") {
		t.Fatalf("expected missing schema error, got %v", err)
	}
}

func TestUsagePerformanceIndexesSupportRepresentativeQueryPlans(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	seedPerformanceIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	assertQueryPlanUsesIndex(t, db, "idx_usage_events_timestamp_id", `
		EXPLAIN QUERY PLAN SELECT id FROM usage_events
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC, id DESC
		LIMIT 50`, "2026-05-01T00:00:00Z", "2026-05-07T00:00:00Z")

	assertQueryPlanUsesIndex(t, db, "idx_usage_events_model", `
		EXPLAIN QUERY PLAN SELECT DISTINCT model FROM usage_events
		ORDER BY model ASC`)

	assertQueryPlanUsesIndex(t, db, "idx_usage_events_auth_index", `
		EXPLAIN QUERY PLAN SELECT id FROM usage_events
		WHERE auth_index = ?`, "authidx-main")

	assertQueryPlanUsesIndex(t, db, "idx_usage_events_api_group_key", `
		EXPLAIN QUERY PLAN SELECT api_group_key, COUNT(*) FROM usage_events
		GROUP BY api_group_key`)

	assertQueryPlanUsesIndex(t, db, "idx_usage_events_auth_type_auth_index_id", `
		EXPLAIN QUERY PLAN SELECT id FROM usage_events
		WHERE auth_type = ? AND auth_index = ? AND id > ?`, "oauth", "authidx-main", 100)

	assertQueryPlanUsesIndex(t, db, "idx_redis_usage_inboxes_status_id", `
		EXPLAIN QUERY PLAN SELECT id FROM redis_usage_inboxes
		WHERE status = ?
		ORDER BY id ASC
		LIMIT 1000`, "pending")

	assertQueryPlanUsesIndex(t, db, "idx_redis_usage_inboxes_status_processed_at", `
		EXPLAIN QUERY PLAN SELECT id FROM redis_usage_inboxes
		WHERE status = ? AND processed_at IS NOT NULL AND processed_at < ?`, "processed", "2026-05-06T00:00:00Z")

	assertQueryPlanUsesIndex(t, db, "idx_redis_usage_inboxes_status_updated_at", `
		EXPLAIN QUERY PLAN SELECT id FROM redis_usage_inboxes
		WHERE status IN (?, ?) AND updated_at < ?`, "decode_failed", "process_failed", "2026-05-06T00:00:00Z")

	assertQueryPlanUsesIndex(t, db, "idx_redis_usage_inboxes_status_usage_event_key", `
		EXPLAIN QUERY PLAN SELECT COUNT(*) FROM redis_usage_inboxes
		WHERE status = ? AND usage_event_key = ?`, "processed", "event-main")

	assertQueryPlanUsesIndex(t, db, "idx_usage_identities_auth_type_name_id", `
		EXPLAIN QUERY PLAN SELECT id FROM usage_identities
		ORDER BY auth_type ASC, name ASC, id ASC`)

	assertQueryPlanUsesIndex(t, db, "idx_usage_identities_auth_type_type", `
		EXPLAIN QUERY PLAN SELECT id FROM usage_identities
		WHERE auth_type = ? AND type IN (?, ?)`, 2, "anthropic", "openai")
}

func sqliteIndexExists(t *testing.T, db *gorm.DB, indexName string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count).Error; err != nil {
		t.Fatalf("check sqlite index %s: %v", indexName, err)
	}
	return count == 1
}

func assertQueryPlanUsesIndex(t *testing.T, db *gorm.DB, indexName string, query string, args ...any) {
	t.Helper()
	var rows []struct {
		ID     int
		Parent int
		Unused int
		Detail string
	}
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		t.Fatalf("explain query plan for %s: %v", indexName, err)
	}
	details := make([]string, 0, len(rows))
	for _, row := range rows {
		details = append(details, row.Detail)
		if strings.Contains(row.Detail, indexName) {
			return
		}
	}
	t.Fatalf("expected query plan to use %s, got %s", indexName, strings.Join(details, " | "))
}
