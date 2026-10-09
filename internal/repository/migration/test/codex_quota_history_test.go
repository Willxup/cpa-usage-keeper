package test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestQuotaHistoryFreshDatabaseCreatesConstrainedGenericSchema(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "fresh-quota-history-schema.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open fresh quota history database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if !db.Migrator().HasTable(&entities.QuotaCycle{}) || !db.Migrator().HasTable(&entities.QuotaPercentSegment{}) {
		t.Fatal("expected generic quota history tables in fresh database")
	}
	if db.Migrator().HasTable("codex_quota_cycles") || db.Migrator().HasTable("codex_quota_percent_segments") {
		t.Fatal("fresh database must not create legacy Codex quota history tables")
	}
	assertQuotaHistoryIndexNames(t, db, "quota_cycles", []string{"uniq_quota_cycles_identity"})
	assertQuotaHistoryIndexNames(t, db, "quota_percent_segments", []string{"uniq_quota_percent_segments_cycle_percent"})
	assertQuotaHistoryRestrictForeignKey(t, db)
	assertQuotaHistoryMigrationApplied(t, db)
	assertQuotaHistoryDatabaseConstraints(t, db)
}

func assertQuotaHistoryDatabaseConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	resetAt := time.Date(2026, 8, 27, 3, 35, 8, 0, time.UTC)
	validCycle := entities.QuotaCycle{
		Provider:        "codex",
		AuthIndex:       "codex-auth",
		QuotaKey:        "rate_limit.primary_window",
		WindowSeconds:   604_800,
		ResetAtSource:   entities.QuotaResetAtSourceAbsolute,
		WindowStartedAt: resetAt.Add(-7 * 24 * time.Hour),
		ResetAt:         resetAt,
		FirstObservedAt: resetAt.Add(-time.Hour),
		LastObservedAt:  resetAt.Add(-time.Minute),
	}
	if err := db.Create(&validCycle).Error; err != nil {
		t.Fatalf("insert valid quota cycle: %v", err)
	}

	for name, mutate := range map[string]func(*entities.QuotaCycle){
		"empty-provider":          func(cycle *entities.QuotaCycle) { cycle.Provider = "" },
		"empty-auth":              func(cycle *entities.QuotaCycle) { cycle.AuthIndex = "" },
		"empty-key":               func(cycle *entities.QuotaCycle) { cycle.QuotaKey = "" },
		"invalid-source":          func(cycle *entities.QuotaCycle) { cycle.ResetAtSource = "guessed" },
		"invalid-seconds":         func(cycle *entities.QuotaCycle) { cycle.WindowSeconds = 0 },
		"invalid-window-bounds":   func(cycle *entities.QuotaCycle) { cycle.WindowStartedAt = cycle.ResetAt },
		"invalid-observed-bounds": func(cycle *entities.QuotaCycle) { cycle.FirstObservedAt = cycle.LastObservedAt.Add(time.Second) },
	} {
		invalid := validCycle
		invalid.ID = 0
		invalid.AuthIndex += "-" + name
		mutate(&invalid)
		if err := db.Create(&invalid).Error; err == nil {
			t.Errorf("expected %s quota cycle to be rejected", name)
		}
	}
	duplicateCycle := validCycle
	duplicateCycle.ID = 0
	if err := db.Create(&duplicateCycle).Error; err == nil {
		t.Fatal("expected duplicate quota cycle identity to be rejected")
	}

	validSegment := entities.QuotaPercentSegment{
		CycleID:          validCycle.ID,
		RemainingPercent: 77,
		FirstObservedAt:  validCycle.FirstObservedAt,
		LastObservedAt:   validCycle.LastObservedAt,
		ObservationCount: 8,
	}
	if err := db.Create(&validSegment).Error; err != nil {
		t.Fatalf("insert valid quota percent segment: %v", err)
	}
	for name, mutate := range map[string]func(*entities.QuotaPercentSegment){
		"negative-percent": func(segment *entities.QuotaPercentSegment) { segment.RemainingPercent = -1 },
		"excess-percent":   func(segment *entities.QuotaPercentSegment) { segment.RemainingPercent = 101 },
		"negative-count":   func(segment *entities.QuotaPercentSegment) { segment.ObservationCount = -1 },
		"reversed-observations": func(segment *entities.QuotaPercentSegment) {
			segment.FirstObservedAt = segment.LastObservedAt.Add(time.Second)
		},
		"missing-cycle": func(segment *entities.QuotaPercentSegment) { segment.CycleID = validCycle.ID + 1000 },
	} {
		invalid := validSegment
		invalid.ID = 0
		invalid.RemainingPercent = 76
		mutate(&invalid)
		if err := db.Create(&invalid).Error; err == nil {
			t.Errorf("expected %s quota percent segment to be rejected", name)
		}
	}
	if err := db.Exec(`INSERT INTO quota_percent_segments
		(cycle_id, remaining_percent, first_observed_at, last_observed_at, observation_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`,
		validCycle.ID, 76, validCycle.FirstObservedAt, validCycle.LastObservedAt, resetAt, resetAt).Error; err == nil {
		t.Fatal("expected explicit zero observation count to be rejected")
	}
	duplicateSegment := validSegment
	duplicateSegment.ID = 0
	if err := db.Create(&duplicateSegment).Error; err == nil {
		t.Fatal("expected duplicate cycle remaining percent to be rejected")
	}
}

func assertQuotaHistoryIndexNames(t *testing.T, db *gorm.DB, table string, expected []string) {
	t.Helper()
	var actual []string
	if err := db.Raw("SELECT name FROM pragma_index_list(?) ORDER BY name", table).Scan(&actual).Error; err != nil {
		t.Fatalf("list %s indexes: %v", table, err)
	}
	if !slices.Equal(actual, expected) {
		t.Fatalf("expected %s indexes %v, got %v", table, expected, actual)
	}
}

func assertQuotaHistoryRestrictForeignKey(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		Table    string `gorm:"column:table"`
		From     string `gorm:"column:from"`
		To       string `gorm:"column:to"`
		OnDelete string `gorm:"column:on_delete"`
	}
	if err := db.Raw("PRAGMA foreign_key_list('quota_percent_segments')").Scan(&rows).Error; err != nil {
		t.Fatalf("list quota percent segment foreign keys: %v", err)
	}
	if len(rows) != 1 || rows[0].Table != "quota_cycles" || rows[0].From != "cycle_id" || rows[0].To != "id" || rows[0].OnDelete != "RESTRICT" {
		t.Fatalf("unexpected quota percent segment foreign keys: %+v", rows)
	}
}

func assertQuotaHistoryMigrationApplied(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", quotaHistoryRebuildMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count quota history rebuild migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one quota history rebuild migration record, got %d", count)
	}
}
