package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateQuotaCycleSnapshotsMigrationCreatesTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := createQuotaCycleSnapshotsMigration(db); err != nil {
		t.Fatalf("create quota_cycle_snapshots table: %v", err)
	}
	if err := createQuotaCycleSnapshotsMigration(db); err != nil {
		t.Fatalf("create quota_cycle_snapshots table should be idempotent: %v", err)
	}

	if !db.Migrator().HasTable(&entities.QuotaCycleSnapshot{}) {
		t.Fatal("expected quota_cycle_snapshots table to exist")
	}
	for _, col := range []string{"provider", "auth_index", "window_seconds", "reset_at", "used_percent", "captured_at"} {
		if !db.Migrator().HasColumn(&entities.QuotaCycleSnapshot{}, col) {
			t.Fatalf("expected quota_cycle_snapshots.%s column to exist", col)
		}
	}
}
