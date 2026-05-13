package migration

import (
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUseInt64PrimaryKeysMigrationAcceptsCurrentSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "current.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open current schema database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("auto migrate current schema: %v", err)
	}

	if err := useInt64PrimaryKeysMigration(db); err != nil {
		t.Fatalf("useInt64PrimaryKeysMigration returned error: %v", err)
	}
}

func TestUseInt64PrimaryKeysMigrationRejectsNonIntegerPrimaryKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "invalid.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open invalid schema database: %v", err)
	}
	defer closeOpenedDatabase(t, db)
	if err := db.Exec(`CREATE TABLE usage_events (id text PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create invalid usage_events table: %v", err)
	}

	err = useInt64PrimaryKeysMigration(db)
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
	if err := createSchemaMigrationsTable(db); err != nil {
		t.Fatalf("create schema migrations table: %v", err)
	}

	if err := runSchemaMigration(db, databaseMigration{version: migrationUseInt64PrimaryKeys, run: useInt64PrimaryKeysMigration}); err != nil {
		t.Fatalf("runSchemaMigration returned error: %v", err)
	}

	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migrationUseInt64PrimaryKeys).Count(&count).Error; err != nil {
		t.Fatalf("count migration row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration row count 1, got %d", count)
	}
}
