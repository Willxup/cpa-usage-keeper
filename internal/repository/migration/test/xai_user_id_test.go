package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
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
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
		if err != nil {
			t.Fatalf("open existing database: %v", err)
		}
		closeMigrationTestDatabase(t, db)
		if err := db.Exec("CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, identity TEXT)").Error; err != nil {
			t.Fatalf("create legacy usage_identities table: %v", err)
		}
		if err := migration.MarkAllAsApplied(db); err != nil {
			t.Fatalf("mark historical migrations applied: %v", err)
		}
		if err := db.Table("schema_migrations").Where("version = ?", xaiUserIDMigrationVersion).Delete(nil).Error; err != nil {
			t.Fatalf("make xAI user id migration pending: %v", err)
		}

		if err := migration.Run(db); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}

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
