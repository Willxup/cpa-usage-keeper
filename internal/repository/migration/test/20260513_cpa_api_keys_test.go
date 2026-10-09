package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunSchemaMigrationCreatesCPAAPIKeySchemaAndRecordsVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "record-cpa-api-keys.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open record schema database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := runLegacyMigration(db, "20260513_create_cpa_api_keys"); err != nil {
		t.Fatalf("runSchemaMigration returned error: %v", err)
	}

	if !db.Migrator().HasTable(&entities.CPAAPIKey{}) {
		t.Fatalf("expected cpa_api_keys table to exist")
	}
	if !sqliteIndexExists(t, db, "uniq_cpa_api_keys_api_key") {
		t.Fatalf("expected api_key unique index to exist")
	}
	if !sqliteIndexExists(t, db, "idx_cpa_api_keys_is_deleted") {
		t.Fatalf("expected is_deleted index to exist")
	}

	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260513_create_cpa_api_keys").Count(&count).Error; err != nil {
		t.Fatalf("count migration row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected migration row count 1, got %d", count)
	}
}
