package test

import (
	"path/filepath"
	"strings"
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

func TestUsageEventStreamStatusCodeMigrationWithLegacyColumns(t *testing.T) {
	const original = "20260919_usage_event_stream_status_code"
	const repair = "20260928_repair_usage_event_stream_status_code"
	for _, tc := range []struct {
		name       string
		pending    string
		existing   string
		failRecord bool
	}{
		{name: "original", pending: original},
		{name: "already marked applied", pending: repair, existing: ", stream NUMERIC"},
		{name: "existing values", pending: repair, existing: ", status_code INTEGER, stream NUMERIC"},
		{name: "case insensitive names", pending: repair, existing: ", STATUS_CODE INTEGER, STREAM NUMERIC"},
		{name: "record failure rolls back", pending: repair, failRecord: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
			if err != nil {
				t.Fatalf("open database: %v", err)
			}
			closeMigrationTestDatabase(t, db)
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				// Match the unquoted column added by the historical migration.
				if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, fail_status_code INTEGER, upstream NUMERIC" + tc.existing + ")").Error; err != nil {
					t.Fatalf("create %s: %v", table, err)
				}
				if err := db.Exec("INSERT INTO " + table + " (id, fail_status_code, upstream) VALUES (1, 502, 1)").Error; err != nil {
					t.Fatalf("seed %s: %v", table, err)
				}
				if strings.Contains(strings.ToLower(tc.existing), "status_code") {
					if err := db.Exec("UPDATE " + table + " SET status_code = 201, stream = 1").Error; err != nil {
						t.Fatalf("seed current fields: %v", err)
					}
				}
			}
			if err := migration.MarkAllAsApplied(db); err != nil {
				t.Fatalf("mark migrations applied: %v", err)
			}
			if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", tc.pending).Error; err != nil {
				t.Fatalf("make migration pending: %v", err)
			}
			if tc.failRecord {
				if err := db.Exec("CREATE TRIGGER reject_repair BEFORE INSERT ON schema_migrations WHEN NEW.version = '" + repair + "' BEGIN SELECT RAISE(ABORT, 'reject repair record'); END").Error; err != nil {
					t.Fatalf("create failure trigger: %v", err)
				}
				if err := migration.Run(db); err == nil || !strings.Contains(err.Error(), "reject repair record") {
					t.Fatalf("expected record failure, got %v", err)
				}
				for _, table := range []string{"usage_events", "usage_events_archive"} {
					var count int64
					if err := db.Raw("SELECT count(*) FROM pragma_table_info(?) WHERE name IN ('status_code', 'stream')", table).Scan(&count).Error; err != nil || count != 0 {
						t.Fatalf("%s columns were not rolled back: count=%d, err=%v", table, count, err)
					}
				}
				var count int64
				if err := db.Table("schema_migrations").Where("version = ?", repair).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("failed repair marked applied: count=%d, err=%v", count, err)
				}
				if err := db.Exec("DROP TRIGGER reject_repair").Error; err != nil {
					t.Fatalf("remove failure trigger: %v", err)
				}
			}
			for range 2 {
				if err := migration.Run(db); err != nil {
					t.Fatalf("Run returned error: %v", err)
				}
			}
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				var row struct {
					FailStatusCode int
					Upstream       bool
					StatusCode     *int
					Stream         *bool
				}
				// Selecting exact names catches HasColumn's suffix false positives.
				if err := db.Table(table).Select("fail_status_code, upstream, status_code AS status_code, stream AS stream").Where("id = 1").Take(&row).Error; err != nil {
					t.Fatalf("read migrated %s: %v", table, err)
				}
				if row.FailStatusCode != 502 || !row.Upstream {
					t.Fatalf("legacy data changed in %s: %+v", table, row)
				}
				if strings.Contains(strings.ToLower(tc.existing), "status_code") && (row.StatusCode == nil || *row.StatusCode != 201 || row.Stream == nil || !*row.Stream) {
					t.Fatalf("current data changed in %s: %+v", table, row)
				}
				if err := db.Exec("INSERT INTO " + table + " (id, status_code, stream) VALUES (2, 200, 1)").Error; err != nil {
					t.Fatalf("write migrated %s: %v", table, err)
				}
			}
			var count int64
			if err := db.Table("schema_migrations").Where("version = ?", tc.pending).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("expected one migration record: count=%d, err=%v", count, err)
			}
		})
	}
}
