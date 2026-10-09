package test

import (
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

const usageEventTraceMetadataMigrationVersion = "20261008_usage_event_trace_metadata"

func TestUsageEventTraceMetadataMigrationPreservesLegacyRowsAndSchemaObjects(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, statement := range []string{
			"CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, request_id TEXT, total_tokens INTEGER)",
			"INSERT INTO " + table + " (id, request_id, total_tokens) VALUES (7, 'existing-request', 123)",
			"CREATE INDEX idx_" + table + "_legacy_request ON " + table + " (request_id)",
			"CREATE TRIGGER guard_" + table + " BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT, 'legacy rows must not be rewritten'); END",
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("seed %s legacy schema: %v", table, err)
			}
		}
	}
	runOnlyMigration(t, db, usageEventTraceMetadataMigrationVersion)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var row struct {
			ID           int64
			RequestID    string
			TotalTokens  int64
			ExecutionID  *string
			TraceID      *string
			NodeKind     *string
			IsFork       *bool
			IsCompaction *bool
		}
		if err := db.Table(table).Take(&row).Error; err != nil {
			t.Fatalf("load migrated %s row: %v", table, err)
		}
		if row.ID != 7 || row.RequestID != "existing-request" || row.TotalTokens != 123 {
			t.Fatalf("legacy %s row changed: %+v", table, row)
		}
		if row.ExecutionID != nil || row.TraceID != nil || row.NodeKind != nil || row.IsFork != nil || row.IsCompaction != nil {
			t.Fatalf("legacy %s metadata must remain unknown: %+v", table, row)
		}
		columns, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatalf("load %s column types: %v", table, err)
		}
		for _, name := range []string{"execution_id", "trace_id", "node_kind", "is_fork", "is_compaction"} {
			found := false
			for _, column := range columns {
				if column.Name() == name {
					found = true
					if nullable, ok := column.Nullable(); !ok || !nullable {
						t.Fatalf("%s.%s must be nullable", table, name)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s.%s column", table, name)
			}
		}
		var objects []string
		if err := db.Raw("SELECT name FROM sqlite_master WHERE tbl_name = ? AND type IN ('index', 'trigger') ORDER BY name", table).Scan(&objects).Error; err != nil {
			t.Fatalf("load preserved %s schema objects: %v", table, err)
		}
		want := []string{"guard_" + table, "idx_" + table + "_legacy_request"}
		if !reflect.DeepEqual(objects, want) {
			t.Fatalf("%s schema objects changed: got %v, want %v", table, objects, want)
		}
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun migrated database: %v", err)
	}
}

func TestUsageEventTraceMetadataMigrationPreservesPartialColumnsAndSkipsMissingTables(t *testing.T) {
	for _, withHotTable := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing tables", true: "partial hot schema"}[withHotTable], func(t *testing.T) {
			db := openUnmigratedTestDatabase(t)
			if withHotTable {
				for _, statement := range []string{
					"CREATE TABLE usage_events (id INTEGER PRIMARY KEY, execution_id TEXT, is_fork NUMERIC)",
					"INSERT INTO usage_events (id, execution_id, is_fork) VALUES (1, 'already-saved', 0)",
				} {
					if err := db.Exec(statement).Error; err != nil {
						t.Fatalf("seed partial schema: %v", err)
					}
				}
			}
			runOnlyMigration(t, db, usageEventTraceMetadataMigrationVersion)
			if db.Migrator().HasTable("usage_events_archive") {
				t.Fatal("metadata migration must not create a missing archive table")
			}
			if !withHotTable {
				return
			}
			for attempt := 0; attempt < 2; attempt++ {
				var row struct {
					ExecutionID  *string
					TraceID      *string
					NodeKind     *string
					IsFork       *bool
					IsCompaction *bool
				}
				if err := db.Table("usage_events").Select("execution_id, trace_id, node_kind, is_fork, is_compaction").Take(&row).Error; err != nil {
					t.Fatalf("load partial schema: %v", err)
				}
				if row.ExecutionID == nil || *row.ExecutionID != "already-saved" || row.IsFork == nil || *row.IsFork || row.TraceID != nil || row.NodeKind != nil || row.IsCompaction != nil {
					t.Fatalf("partial metadata changed: %+v", row)
				}
				if attempt == 0 {
					makeTraceMetadataMigrationPending(t, db)
					if err := migration.Run(db); err != nil {
						t.Fatalf("rerun metadata column migration: %v", err)
					}
				}
			}
		})
	}
}

func makeTraceMetadataMigrationPending(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Table("schema_migrations").Where("version = ?", usageEventTraceMetadataMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make trace metadata migration pending: %v", err)
	}
}
