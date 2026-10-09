package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const quotaHistoryRebuildMigrationVersion = "20260822_rebuild_quota_history"

func TestQuotaHistoryRebuildMigrationDropsWrongRowsAndIsIdempotent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "upgrade-quota-history.db")
	db, err := gorm.Open(sqlite.Open(databasePath+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open quota history upgrade database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if err := db.AutoMigrate(&entities.CodexQuotaCycle{}, &entities.CodexQuotaPercentSegment{}); err != nil {
		t.Fatalf("create legacy quota history tables: %v", err)
	}
	resetAt := time.Date(2026, 8, 27, 3, 35, 8, 0, time.UTC)
	legacyCycle := entities.CodexQuotaCycle{
		AuthIndex:       "legacy-codex-auth",
		WindowRole:      entities.CodexQuotaWindowRolePrimary,
		WindowSeconds:   604_800,
		ResetAtSource:   entities.CodexQuotaResetAtSourceAbsolute,
		WindowStartedAt: resetAt.Add(-7 * 24 * time.Hour),
		ResetAt:         resetAt,
		FirstObservedAt: resetAt.Add(-time.Hour),
		LastObservedAt:  resetAt.Add(-time.Minute),
	}
	if err := db.Create(&legacyCycle).Error; err != nil {
		t.Fatalf("insert legacy quota cycle: %v", err)
	}
	legacySegment := entities.CodexQuotaPercentSegment{
		CycleID:             legacyCycle.ID,
		RemainingPercent:    77,
		FirstRawUsedPercent: 23,
		LastRawUsedPercent:  23,
		FirstObservedAt:     legacyCycle.FirstObservedAt,
		LastObservedAt:      legacyCycle.LastObservedAt,
		ObservationCount:    8,
	}
	if err := db.Create(&legacySegment).Error; err != nil {
		t.Fatalf("insert legacy quota percent segment: %v", err)
	}
	runOnlyMigration(t, db, quotaHistoryRebuildMigrationVersion)

	if db.Migrator().HasTable("codex_quota_cycles") || db.Migrator().HasTable("codex_quota_percent_segments") {
		t.Fatal("expected legacy Codex quota history tables to be dropped")
	}
	if !db.Migrator().HasTable("quota_cycles") || !db.Migrator().HasTable("quota_percent_segments") {
		t.Fatal("expected generic quota history tables after rebuild migration")
	}
	for _, table := range []string{"quota_cycles", "quota_percent_segments"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count rebuilt %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected rebuilt %s to be empty, got %d rows", table, count)
		}
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun quota history rebuild migration: %v", err)
	}
}
