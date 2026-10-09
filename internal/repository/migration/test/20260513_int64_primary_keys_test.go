package test

import (
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUseInt64PrimaryKeysMigrationRejectsNonIntegerPrimaryKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "invalid.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open invalid schema database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec(`CREATE TABLE usage_events (id text PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create invalid usage_events table: %v", err)
	}

	err = runLegacyMigration(db, "20260513_use_int64_primary_keys")
	if err == nil {
		t.Fatal("expected migration to reject non-integer primary key")
	}
	if !strings.Contains(err.Error(), "table usage_events id column is not an integer primary key") {
		t.Fatalf("expected usage_events primary key error, got %v", err)
	}
}

func TestRunSchemaMigrationRecordsInt64PrimaryKeyMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "record.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open record schema database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("auto migrate current schema: %v", err)
	}

	if err := runLegacyMigration(db, "20260513_use_int64_primary_keys"); err != nil {
		t.Fatalf("runSchemaMigration returned error: %v", err)
	}

	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260513_use_int64_primary_keys").Count(&count).Error; err != nil {
		t.Fatalf("count migration row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration row count 1, got %d", count)
	}
}
