package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openUnmigratedTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	return db
}

func runOnlyMigration(t *testing.T, db *gorm.DB, version string) {
	t.Helper()
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatalf("mark migration %s pending: %v", version, err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run migration %s: %v", version, err)
	}
}

func closeMigrationTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sql database: %v", err)
		}
	})
}
