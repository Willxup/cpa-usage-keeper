package test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageEventArchiveMigrationVersion = "20260730_create_usage_event_archive"

func TestUsageEventArchiveMigrationCreatesColdTableOnExistingDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "existing.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	if err := db.AutoMigrate(&entities.UsageEvent{}); err != nil {
		t.Fatalf("create existing usage_events: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", usageEventArchiveMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make archive migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !db.Migrator().HasTable("usage_events_archive") {
		t.Fatal("expected archive migration to create usage_events_archive")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageEventArchiveMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count archive migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected archive migration recorded once, got %d", count)
	}
}

func TestUsageEventReplayPagesMergeArchiveAndHotByGlobalID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "replay.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open replay database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	if err := db.AutoMigrate(&entities.UsageEvent{}, &entities.UsageEventArchive{}); err != nil {
		t.Fatalf("create replay schema: %v", err)
	}
	now := time.Date(2026, 7, 30, 4, 30, 0, 0, time.Local)
	archiveRows := []entities.UsageEventArchive{
		{ID: 1, EventKey: "archive-1", AuthType: "oauth", AuthIndex: "auth-file-1", Source: "archive-source", Timestamp: now.AddDate(0, 0, -120), TotalTokens: 1},
		{ID: 4, EventKey: "late-archive-4", Timestamp: now.AddDate(0, 0, -100), TotalTokens: 4},
	}
	hotRows := []entities.UsageEvent{
		{ID: 2, EventKey: "hot-2", AuthType: "apikey", AuthIndex: "provider-2", Source: "hot-source", Timestamp: now.AddDate(0, 0, -10), TotalTokens: 2},
		{ID: 3, EventKey: "hot-3", Timestamp: now.AddDate(0, 0, -9), TotalTokens: 3},
		{ID: 5, EventKey: "hot-5", Timestamp: now, TotalTokens: 5},
	}
	if err := db.Create(&archiveRows).Error; err != nil {
		t.Fatalf("seed archive replay rows: %v", err)
	}
	if err := db.Create(&hotRows).Error; err != nil {
		t.Fatalf("seed hot replay rows: %v", err)
	}

	targetID, err := migration.LoadUsageAggregationReplayTargetEventID(db)
	if err != nil {
		t.Fatalf("load replay target: %v", err)
	}
	if targetID != 5 {
		t.Fatalf("expected replay target 5, got %d", targetID)
	}
	var gotIDs []int64
	gotEvents := make(map[int64]entities.UsageEvent)
	afterID := int64(0)
	for {
		page, err := migration.LoadUsageAggregationReplayEventPage(db, afterID, targetID, 2)
		if err != nil {
			t.Fatalf("load replay page after %d: %v", afterID, err)
		}
		if len(page) == 0 {
			break
		}
		for _, event := range page {
			gotIDs = append(gotIDs, event.ID)
			gotEvents[event.ID] = event
		}
		afterID = page[len(page)-1].ID
	}
	if fmt.Sprint(gotIDs) != fmt.Sprint([]int64{1, 2, 3, 4, 5}) {
		t.Fatalf("expected globally ordered replay IDs, got %v", gotIDs)
	}
	if event := gotEvents[1]; event.AuthType != "oauth" || event.AuthIndex != "auth-file-1" || event.Source != "archive-source" {
		t.Fatalf("expected archive replay to preserve identity fields, got %+v", event)
	}
	if event := gotEvents[2]; event.AuthType != "apikey" || event.AuthIndex != "provider-2" || event.Source != "hot-source" {
		t.Fatalf("expected hot replay to preserve identity fields, got %+v", event)
	}
}

func TestUsageEventReplayQueryUsesPrimaryKeyMerge(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "plan.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open replay plan database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	if err := db.AutoMigrate(&entities.UsageEvent{}, &entities.UsageEventArchive{}); err != nil {
		t.Fatalf("create replay plan schema: %v", err)
	}
	var rows []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := db.Raw(`EXPLAIN QUERY PLAN
		SELECT id FROM (
			SELECT id FROM usage_events_archive WHERE id > ? AND id <= ?
			UNION ALL
			SELECT id FROM usage_events WHERE id > ? AND id <= ?
		) ORDER BY id ASC LIMIT ?`, 0, 100, 0, 100, 10).Scan(&rows).Error; err != nil {
		t.Fatalf("explain replay query: %v", err)
	}
	details := make([]string, 0, len(rows))
	for _, row := range rows {
		details = append(details, row.Detail)
	}
	plan := strings.Join(details, "\n")
	for _, want := range []string{"MERGE (UNION ALL)", "usage_events_archive USING INTEGER PRIMARY KEY", "usage_events USING INTEGER PRIMARY KEY"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("expected replay query plan to contain %q, got:\n%s", want, plan)
		}
	}
}
