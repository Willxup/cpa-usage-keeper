package test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

const quotaHistoryResetMigrationVersion = "20260827_reset_quota_history"

func TestQuotaHistoryResetMigrationBacksUpThenClearsHistory(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		SQLitePath: filepath.Join(root, "app.db"),
		BackupDir:  filepath.Join(root, "backups"),
	}
	db, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	seedQuotaHistoryResetRows(t, db)
	markQuotaHistoryResetPending(t, db)
	closeQuotaHistoryResetDatabase(t, db)

	upgraded, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("open upgraded database: %v", err)
	}
	defer closeQuotaHistoryResetDatabase(t, upgraded)
	assertQuotaHistoryResetRows(t, upgraded, 0, 0)
	assertQuotaHistoryResetVersion(t, upgraded, 1)

	files, err := backup.ListFiles(cfg.BackupDir)
	if err != nil {
		t.Fatalf("list migration backups: %v", err)
	}
	if len(files) != 1 || !strings.HasPrefix(filepath.Base(files[0]), "database_") || filepath.Ext(files[0]) != ".db" {
		t.Fatalf("expected one standard database backup, got %+v", files)
	}
	assertQuotaHistoryResetBackupRows(t, files[0], 1, 1)
}

func TestQuotaHistoryResetMigrationStopsWhenBackupFails(t *testing.T) {
	root := t.TempDir()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(root, "app.db")})
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	defer closeQuotaHistoryResetDatabase(t, db)
	seedQuotaHistoryResetRows(t, db)
	markQuotaHistoryResetPending(t, db)

	backupFailure := errors.New("expected backup failure")
	err = migration.Run(db, migration.RunOptions{
		BeforeDestructiveMigration: func(context.Context, string) error { return backupFailure },
	})
	if !errors.Is(err, backupFailure) {
		t.Fatalf("expected backup failure, got %v", err)
	}
	assertQuotaHistoryResetRows(t, db, 1, 1)
	assertQuotaHistoryResetVersion(t, db, 0)
}

func seedQuotaHistoryResetRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	cycle := entities.QuotaCycle{
		Provider: "codex", AuthIndex: "codex-auth", QuotaKey: "rate_limit.primary_window",
		WindowSeconds: 604_800, ResetAtSource: entities.QuotaResetAtSourceAbsolute,
		WindowStartedAt: now.Add(-24 * time.Hour), ResetAt: now.Add(6 * 24 * time.Hour),
		FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Minute),
	}
	if err := db.Create(&cycle).Error; err != nil {
		t.Fatalf("seed quota cycle: %v", err)
	}
	segment := entities.QuotaPercentSegment{
		CycleID: cycle.ID, RemainingPercent: 77, FirstObservedAt: cycle.FirstObservedAt,
		LastObservedAt: cycle.LastObservedAt, ObservationCount: 2,
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatalf("seed quota percent segment: %v", err)
	}
}

func markQuotaHistoryResetPending(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", quotaHistoryResetMigrationVersion).Error; err != nil {
		t.Fatalf("mark quota history reset pending: %v", err)
	}
}

func assertQuotaHistoryResetRows(t *testing.T, db *gorm.DB, cycles, segments int64) {
	t.Helper()
	for table, want := range map[string]int64{"quota_cycles": cycles, "quota_percent_segments": segments} {
		var got int64
		if err := db.Table(table).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("expected %s row count %d, got %d", table, want, got)
		}
	}
}

func assertQuotaHistoryResetVersion(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var got int64
	if err := db.Table("schema_migrations").Where("version = ?", quotaHistoryResetMigrationVersion).Count(&got).Error; err != nil {
		t.Fatalf("count reset migration version: %v", err)
	}
	if got != want {
		t.Fatalf("expected migration version count %d, got %d", want, got)
	}
}

func assertQuotaHistoryResetBackupRows(t *testing.T, path string, cycles, segments int64) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open migration backup: %v", err)
	}
	defer db.Close()
	for table, want := range map[string]int64{"quota_cycles": cycles, "quota_percent_segments": segments} {
		var got int64
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatalf("count backup %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("expected backup %s row count %d, got %d", table, want, got)
		}
	}
}

func closeQuotaHistoryResetDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql database: %v", err)
	}
}
