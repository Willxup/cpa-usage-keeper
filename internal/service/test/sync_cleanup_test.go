package test

import (
	"bytes"
	"context"
	"path/filepath"
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

func TestSyncServiceCleanupStorageDefersArchiveUntilAggregationsCatchUp(t *testing.T) {
	db := openSyncCleanupTestDatabase(t)
	now := time.Date(2026, 6, 16, 9, 0, 0, 0, time.Local)
	seedSyncCleanupUsageEventsAt(t, db, now.AddDate(0, 0, -91), now.Add(-time.Hour))
	logs := captureSyncCleanupLogs(t, logrus.WarnLevel)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		Now: func() time.Time { return now },
	})

	if err := syncer.CleanupStorage(context.Background()); err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}

	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected usage_events to be retained by default, got %d rows", count)
	}
	content := logs.String()
	if !strings.Contains(content, "level=warning") || !strings.Contains(content, "usage event archive deferred because aggregations are lagging") {
		t.Fatalf("expected aggregation lag warning, got %q", content)
	}
}

func TestSyncServiceCleanupStorageArchivesUsageEventsAfterAggregationsCatchUp(t *testing.T) {
	// 准备：写入一条过期和一条近期事件，并先追平两个全局聚合 checkpoint。
	db := openSyncCleanupTestDatabase(t)
	now := time.Date(2026, 6, 16, 9, 0, 0, 0, time.Local)
	seedSyncCleanupUsageEventsAt(t, db, now.AddDate(0, 0, -91), now.Add(-time.Hour))
	catchUpSyncCleanupAggregations(t, db, now)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{Now: func() time.Time { return now }})

	// 执行：固定维护策略把超过 90 天且已完成聚合的 raw event 移入 archive。
	if err := syncer.CleanupStorage(context.Background()); err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}

	// 断言：安全水位满足后 hot 只保留近期事件，旧事件完整进入 archive。
	var remainingKeys []string
	if err := db.Model(&entities.UsageEvent{}).Order("event_key asc").Pluck("event_key", &remainingKeys).Error; err != nil {
		t.Fatalf("load remaining usage events: %v", err)
	}
	if len(remainingKeys) != 1 || remainingKeys[0] != "recent" {
		t.Fatalf("expected only recent usage event to remain, got %v", remainingKeys)
	}
	var archivedKeys []string
	if err := db.Model(&entities.UsageEventArchive{}).Order("event_key asc").Pluck("event_key", &archivedKeys).Error; err != nil {
		t.Fatalf("load archived usage events: %v", err)
	}
	if len(archivedKeys) != 1 || archivedKeys[0] != "old" {
		t.Fatalf("expected old usage event in archive, got %v", archivedKeys)
	}
}

func TestNewSyncServiceCleanupStorageUsesFixedArchivePolicy(t *testing.T) {
	// 准备：通过生产构造器创建 service，并让三个全局聚合 checkpoint 先追平。
	db := openSyncCleanupTestDatabase(t)
	now := time.Now().In(time.Local)
	seedSyncCleanupUsageEventsAt(t, db, now.AddDate(0, 0, -91), now)
	catchUpSyncCleanupAggregations(t, db, now)
	syncer := service.NewSyncService(db, config.Config{
		CPABaseURL:       "https://cpa.example.com",
		CPAManagementKey: "secret",
		RequestTimeout:   time.Second,
	})

	// 执行：调用生产 SyncService 的统一维护入口。
	if err := syncer.CleanupStorage(context.Background()); err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}

	// 断言：无需配置开关，固定策略只把安全水位内的过期事件移出 hot。
	var remainingKeys []string
	if err := db.Model(&entities.UsageEvent{}).Order("event_key asc").Pluck("event_key", &remainingKeys).Error; err != nil {
		t.Fatalf("load remaining usage events: %v", err)
	}
	if len(remainingKeys) != 1 || remainingKeys[0] != "recent" {
		t.Fatalf("expected fixed archive policy to retain only recent usage event, got %v", remainingKeys)
	}
}

func openSyncCleanupTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("load sql db: %v", err)
		}
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
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
	// 准备：helper 把 cleanup 依赖的三个全局 cursor 推进到当前最大 event ID。
	t.Helper()
	// 执行：先保持旧 Overview 最终结果，再写入 Activity 独立结果。
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate overview before cleanup: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate activity before cleanup: %v", err)
	}
	if err := repository.AggregateUsageLatencyStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate latency before cleanup: %v", err)
	}
	// 断言由调用用例通过最终归档结果完成，helper 不额外读取数据库。
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
