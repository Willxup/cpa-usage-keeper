package test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestSyncServiceCleanupStorageArchivesOnlyAfterAggregationsCatchUp(t *testing.T) {
	for _, productionConstructor := range []bool{false, true} {
		name := "fixed clock"
		if productionConstructor {
			name = "production constructor"
		}
		t.Run(name, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			now := time.Date(2026, 6, 16, 9, 0, 0, 0, time.Local)
			var syncer *service.SyncService
			if productionConstructor {
				now = time.Now()
				syncer = service.NewSyncService(db, config.Config{
					CPABaseURL:       "https://cpa.example.com",
					CPAManagementKey: "secret",
					RequestTimeout:   time.Second,
				})
			} else {
				syncer = service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{Now: func() time.Time { return now }})
			}
			seedSyncCleanupUsageEventsAt(t, db, now.AddDate(0, 0, -91), now.Add(-time.Hour))
			logs := captureSyncCleanupLogs(t, logrus.WarnLevel)

			if err := syncer.CleanupStorage(context.Background()); err != nil {
				t.Fatalf("CleanupStorage before catch-up: %v", err)
			}
			var count int64
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
				t.Fatalf("count usage events: %v", err)
			}
			if count != 2 {
				t.Fatalf("expected both usage events to be retained before catch-up, got %d", count)
			}
			content := logs.String()
			if !strings.Contains(content, "level=warning") || !strings.Contains(content, "usage event archive deferred because aggregations are lagging") {
				t.Fatalf("expected aggregation lag warning, got %q", content)
			}

			// 三个聚合追平后，超过 90 天的事件才能从 hot 移入 archive。
			catchUpSyncCleanupAggregations(t, db, now)
			if err := syncer.CleanupStorage(context.Background()); err != nil {
				t.Fatalf("CleanupStorage after catch-up: %v", err)
			}
			var remainingKeys, archivedKeys []string
			if err := db.Model(&entities.UsageEvent{}).Pluck("event_key", &remainingKeys).Error; err != nil {
				t.Fatalf("load remaining usage events: %v", err)
			}
			if len(remainingKeys) != 1 || remainingKeys[0] != "recent" {
				t.Fatalf("expected only recent event in hot storage, got %v", remainingKeys)
			}
			if err := db.Model(&entities.UsageEventArchive{}).Pluck("event_key", &archivedKeys).Error; err != nil {
				t.Fatalf("load archived usage events: %v", err)
			}
			if len(archivedKeys) != 1 || archivedKeys[0] != "old" {
				t.Fatalf("expected old usage event in archive, got %v", archivedKeys)
			}
		})
	}
}

func seedSyncCleanupUsageEventsAt(t *testing.T, db *gorm.DB, oldAt, recentAt time.Time) {
	t.Helper()
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "old", Model: "claude-sonnet", Timestamp: oldAt, TotalTokens: 1},
		{EventKey: "recent", Model: "claude-sonnet", Timestamp: recentAt, TotalTokens: 2},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
}

func catchUpSyncCleanupAggregations(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate overview before cleanup: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate activity before cleanup: %v", err)
	}
	if err := repository.AggregateUsageLatencyStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate latency before cleanup: %v", err)
	}
}

func captureSyncCleanupLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(level)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return logs
}

func TestSyncServiceArchiveRetention(t *testing.T) {
	for _, production := range []bool{false, true} {
		t.Run(fmt.Sprint(production), func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			now := time.Now()
			syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{Now: func() time.Time { return now }, UsageRawRetentionDays: 90})
			if production {
				syncer = service.NewSyncService(db, config.Config{UsageRawRetentionDays: 90})
			}
			seedSyncCleanupUsageEventsAt(t, db, now.AddDate(0, 0, -100), now.AddDate(0, 0, -60))
			cold := entities.UsageEventArchive{ID: 1000, EventKey: "previous-archive", Timestamp: now.AddDate(0, 0, -200)}
			if err := db.Create(&cold).Error; err != nil {
				t.Fatal(err)
			}
			logs := captureSyncCleanupLogs(t, logrus.DebugLevel)
			if err := syncer.CleanupStorage(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatal("lagging hot events must remain")
			}
			catchUpSyncCleanupAggregations(t, db, now)
			if err := syncer.CleanupStorage(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entities.UsageEventArchive{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("expired archive retained: %d", count)
			}
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("expected 60-day hot event retained, got %d", count)
			}
			if !strings.Contains(logs.String(), "usage_events_archive_deleted=1") {
				t.Fatal("missing deletion count")
			}
		})
	}
}
