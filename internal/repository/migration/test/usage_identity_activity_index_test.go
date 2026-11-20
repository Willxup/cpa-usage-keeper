package test

import (
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

const usageIdentityActivityIndexVersion = "20261103_usage_identity_activity_index"

func TestUsageIdentityActivityIndexFreshAndUpgrade(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		t.Run(map[bool]string{true: "fresh", false: "upgrade"}[fresh], func(t *testing.T) {
			var db *gorm.DB
			if fresh {
				var err error
				db, err = repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
				if err != nil {
					t.Fatal(err)
				}
				closeMigrationTestDatabase(t, db)
			} else {
				db = openUnmigratedTestDatabase(t)
				if err := db.Exec(`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, auth_type TEXT, auth_index TEXT, failed INTEGER, timestamp TEXT)`).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Exec(`INSERT INTO usage_events VALUES (1, 'oauth', 'auth', 0, '2026-01-01T12:00:00.1Z')`).Error; err != nil {
					t.Fatal(err)
				}
				if err := migration.MarkAllAsApplied(db); err != nil {
					t.Fatal(err)
				}
				makeIdentityActivityIndexMigrationPending(t, db)
			}
			for pass := 0; pass < 2; pass++ {
				if pass > 0 {
					makeIdentityActivityIndexMigrationPending(t, db)
				}
				if err := migration.Run(db); err != nil {
					t.Fatal(err)
				}
				var definition string
				if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", entities.UsageIdentityActivityIndex).Scan(&definition).Error; err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(definition, "auth_type, auth_index, failed") || !strings.Contains(definition, entities.UsageIdentityActivityTimestamp) {
					t.Fatalf("missing activity expression index: %s", definition)
				}
				var count int64
				if err := db.Table("schema_migrations").Where("version = ?", usageIdentityActivityIndexVersion).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("migration version count %d: %v", count, err)
				}
				if !fresh {
					if err := db.Table("usage_events").Count(&count).Error; err != nil || count != 1 {
						t.Fatalf("existing event was changed: %d %v", count, err)
					}
				}
			}
		})
	}
}

func TestUsageIdentityActivityIndexMigrationRollsBackOnFailure(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	if err := db.Exec(`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, auth_type TEXT, auth_index TEXT, timestamp TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO usage_events VALUES (1, 'oauth', 'auth', '2026-01-01T12:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	makeIdentityActivityIndexMigrationPending(t, db)
	if err := migration.Run(db); err == nil {
		t.Fatal("expected missing failed column to abort migration")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageIdentityActivityIndexVersion).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed migration recorded: %d %v", count, err)
	}
	if db.Migrator().HasIndex(&entities.UsageEvent{}, entities.UsageIdentityActivityIndex) {
		t.Fatal("failed migration left an index")
	}
	if err := db.Table("usage_events").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("failed migration changed events: %d %v", count, err)
	}
}

func makeIdentityActivityIndexMigrationPending(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Table("schema_migrations").Where("version = ?", usageIdentityActivityIndexVersion).Delete(nil).Error; err != nil {
		t.Fatal(err)
	}
}
