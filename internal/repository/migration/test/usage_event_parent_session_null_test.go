package test

import (
	"database/sql"
	"testing"

	"gorm.io/gorm"
)

const usageEventParentSessionNullMigrationVersion = "20260922_normalize_usage_event_parent_session_null"

func TestUsageEventParentSessionNullMigrationNormalizesHotAndArchiveRows(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, event_key TEXT NOT NULL, parent_session_id TEXT)").Error; err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
		if err := db.Exec("INSERT INTO "+table+" (id, event_key, parent_session_id) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)",
			1, "existing-null", nil,
			2, "empty-parent", "",
			3, "real-parent", "parent-session",
		).Error; err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}

	for run := 1; run <= 2; run++ {
		runOnlyMigration(t, db, usageEventParentSessionNullMigrationVersion)
		for _, table := range []string{"usage_events", "usage_events_archive"} {
			assertParentSessionMigrationRows(t, db, table, run)
		}
	}
}

func TestUsageEventParentSessionNullMigrationSkipsMissingTablesAndColumns(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	if err := db.Exec("CREATE TABLE usage_events (id INTEGER PRIMARY KEY, event_key TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("create usage_events without parent session column: %v", err)
	}
	runOnlyMigration(t, db, usageEventParentSessionNullMigrationVersion)
}

func assertParentSessionMigrationRows(t *testing.T, db *gorm.DB, table string, run int) {
	t.Helper()
	rows, err := db.Raw("SELECT id, event_key, parent_session_id FROM " + table + " ORDER BY id").Rows()
	if err != nil {
		t.Fatalf("read %s after migration run %d: %v", table, run, err)
	}
	defer rows.Close()

	type wantRow struct {
		id        int64
		eventKey  string
		parent    string
		parentSet bool
	}
	want := []wantRow{
		{id: 1, eventKey: "existing-null"},
		{id: 2, eventKey: "empty-parent"},
		{id: 3, eventKey: "real-parent", parent: "parent-session", parentSet: true},
	}
	index := 0
	for rows.Next() {
		if index >= len(want) {
			t.Fatalf("%s has unexpected extra row after migration run %d", table, run)
		}
		var id int64
		var eventKey string
		var parent sql.NullString
		if err := rows.Scan(&id, &eventKey, &parent); err != nil {
			t.Fatalf("scan %s after migration run %d: %v", table, run, err)
		}
		expected := want[index]
		if id != expected.id || eventKey != expected.eventKey || parent.Valid != expected.parentSet || parent.String != expected.parent {
			t.Fatalf("unexpected %s row after migration run %d: id=%d event_key=%q parent=%+v", table, run, id, eventKey, parent)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s after migration run %d: %v", table, run, err)
	}
	if index != len(want) {
		t.Fatalf("%s row count after migration run %d = %d, want %d", table, run, index, len(want))
	}
}
