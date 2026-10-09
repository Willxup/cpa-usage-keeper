package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

const xaiUserIDMigrationVersion = "20260711_add_usage_identity_xai_user_id"

func TestUsageIdentityXAIUserIDMigrationSupportsFreshAndExistingDatabases(t *testing.T) {
	t.Run("fresh database", func(t *testing.T) {
		db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "fresh.db")})
		if err != nil {
			t.Fatalf("OpenDatabase returned error: %v", err)
		}
		closeMigrationTestDatabase(t, db)

		assertXAIUserIDMigrationApplied(t, db)
	})

	t.Run("existing database", func(t *testing.T) {
		db := openUnmigratedTestDatabase(t)
		if err := db.Exec("CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, identity TEXT)").Error; err != nil {
			t.Fatalf("create legacy usage_identities table: %v", err)
		}
		runOnlyMigration(t, db, xaiUserIDMigrationVersion)

		assertXAIUserIDMigrationApplied(t, db)
	})
}

func assertXAIUserIDMigrationApplied(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasColumn("usage_identities", "xai_user_id") {
		t.Fatal("expected usage_identities.xai_user_id column")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", xaiUserIDMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count xAI user id migration record: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one xAI user id migration record, got %d", count)
	}
}
