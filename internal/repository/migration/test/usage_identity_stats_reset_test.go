package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUsageIdentityStatsResetMigrationPreservesExistingTotalsAndBaselines(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationTestDatabase(t, db)
	// 旧 schema 允许计数为 NULL；新基线默认零，不改变已有累计。
	if err := db.Exec("CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, total_requests INTEGER, success_count INTEGER, failure_count INTEGER, input_tokens INTEGER, cache_read_tokens INTEGER, total_tokens INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO usage_identities VALUES (1, 10, 8, 2, 600, 300, 1000), (2, NULL, NULL, NULL, NULL, NULL, NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	rerun := func() {
		t.Helper()
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "20260910_usage_identity_stats_reset").Error; err != nil {
			t.Fatal(err)
		}
		if err := migration.Run(db); err != nil {
			t.Fatal(err)
		}
	}
	rerun()
	var row entities.UsageIdentity
	if err := db.First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.TotalRequests != 10 || row.TotalTokens != 1000 || row.ResetTotalRequests != 0 || row.StatsResetAt != nil {
		t.Fatalf("migration changed lifetime/default: %+v", row)
	}
	now := time.Date(2026, 9, 10, 16, 0, 0, 0, time.FixedZone("project", 8*60*60))
	for _, id := range []int64{1, 2} {
		if err := repository.ResetUsageIdentityStats(context.Background(), db, id, now); err != nil {
			t.Fatal(err)
		}
	}
	rerun()
	row = entities.UsageIdentity{}
	if err := db.First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.TotalRequests != 10 || row.ResetTotalRequests != 10 || row.ResetTotalTokens != 1000 || row.StatsResetAt == nil || !row.StatsResetAt.Equal(now) {
		t.Fatalf("rerun lost baseline: %+v", row)
	}
}
