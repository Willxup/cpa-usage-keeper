package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// newInitializedApp 供只需业务 wiring 的测试走与 RunContext 相同的 M0–M8 初始化，不启动网络接收。
func newInitializedApp(t *testing.T, cfg config.Config) *keeperapp.App {
	t.Helper()
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if err := application.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return application
}

// TestPricingStartupShellPrecedesDatabase 验证启动外壳构造时尚未触碰业务库，旧库保护仍有机会先执行。
func TestPricingStartupShellPrecedesDatabase(t *testing.T) {
	cfg := testAppConfig(t)
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if _, err := os.Stat(cfg.SQLitePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database opened before startup shell: %v", err)
	}
	if application.DB != nil || application.Router != nil || application.StartupShell == nil {
		t.Fatalf("business services were initialized before startup shell: db=%v router=%v shell=%v", application.DB, application.Router, application.StartupShell)
	}
	status := httptest.NewRecorder()
	application.StartupShell.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/startup/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"phase":"opening"`) {
		t.Fatalf("startup shell status = %d %s", status.Code, status.Body.String())
	}
	business := httptest.NewRecorder()
	application.StartupShell.ServeHTTP(business, httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil))
	if business.Code != http.StatusServiceUnavailable || !strings.Contains(business.Body.String(), `"code":"migration_in_progress"`) {
		t.Fatalf("business route before initialization = %d %s", business.Code, business.Body.String())
	}
}

// TestPricingStartupTLSFailureLeavesDatabaseUntouched 验证 TLS 外壳不能服务时不会开始旧库迁移。
func TestPricingStartupTLSFailureLeavesDatabaseUntouched(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppHost, cfg.AppPort = "127.0.0.1", "0"
	cfg.TLSEnabled = true
	cfg.TLSCertFile = t.TempDir() + "/missing-cert.pem"
	cfg.TLSKeyFile = t.TempDir() + "/missing-key.pem"
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if err := application.RunContext(context.Background()); err == nil || !strings.Contains(err.Error(), "load startup TLS certificate") {
		t.Fatalf("TLS startup failure = %v", err)
	}
	if _, err := os.Stat(cfg.SQLitePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database opened despite TLS startup failure: %v", err)
	}
}

// TestPricingFreshInitializationKeepsRuntimeGate 验证新库初始化只标记数据完成，完整路由仍等运行环境切换。
func TestPricingFreshInitializationKeepsRuntimeGate(t *testing.T) {
	cfg := testAppConfig(t)
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if err := application.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if application.DB == nil || application.Router == nil || application.PricingService == nil {
		t.Fatal("business services were not prepared after fresh initialization")
	}
	var state entities.PricingMigrationState
	if err := application.DB.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.InitKind != "fresh" || !state.SchemaComplete || !state.DataComplete || state.Phase != "data_complete" {
		t.Fatalf("fresh completion = %+v", state)
	}
	status := httptest.NewRecorder()
	application.StartupShell.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/startup/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"phase":"migrating"`) {
		t.Fatalf("shell switched before runtime ready: %d %s", status.Code, status.Body.String())
	}
}

// TestPricingFreshBootstrapRestartKeepsInbox 验证只建控制表和 inbox 后退出，重启仍按 fresh 初始化并保留已接收消息。
func TestPricingFreshBootstrapRestartKeepsInbox(t *testing.T) {
	cfg := testAppConfig(t)
	writer, reader, err := repository.OpenUnmigratedDatabasePools(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.BootstrapPricingInitialization(context.Background(), writer); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), writer); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), writer, "http_pull", []string{`{"usage":"one"}`}, time.Now()); err != nil {
		t.Fatal(err)
	}
	closePricingStartupPools(t, writer, reader)

	application := newInitializedApp(t, cfg)
	var state entities.PricingMigrationState
	if err := application.DB.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.InitKind != "fresh" || !state.SchemaComplete || !state.DataComplete || state.BackupPath != nil {
		t.Fatalf("fresh restart changed identity or performed legacy backup: %+v", state)
	}
	var count int64
	if err := application.DB.Table("redis_usage_inboxes").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("received message after fresh restart = %d, %v", count, err)
	}
}

// TestPricingCompletedStartupRunsPendingSchemaVersion 验证数据已完成的库仍会执行剩余普通版本，而不重做费用回填。
func TestPricingCompletedStartupRunsPendingSchemaVersion(t *testing.T) {
	cfg := testAppConfig(t)
	seed, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const version = "20260905_usage_event_api_group_key_timestamp_index"
	if err := seed.Exec("DROP INDEX IF EXISTS idx_usage_events_api_group_key_timestamp").Error; err != nil {
		t.Fatal(err)
	}
	if err := seed.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	closePricingStartupPools(t, seed, seed)

	application := newInitializedApp(t, cfg)
	var count int64
	if err := application.DB.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("pending version after startup = %d, %v", count, err)
	}
	if !application.DB.Migrator().HasIndex("usage_events", "idx_usage_events_api_group_key_timestamp") {
		t.Fatal("pending index migration did not run after pricing data completion")
	}
	var state entities.PricingMigrationState
	if err := application.DB.Where("id = ?", 1).Take(&state).Error; err != nil || state.InitKind != "fresh" || state.BackupPath != nil {
		t.Fatalf("completed pricing identity changed while running pending migration: %+v, %v", state, err)
	}
}

// TestPricingLegacyStartupProtectsAndBackfillsOldPhysicalColumns 验证旧库先备份，再按固定价格回填明细及既有桶。
func TestPricingLegacyStartupProtectsAndBackfillsOldPhysicalColumns(t *testing.T) {
	cfg := testAppConfig(t)
	seed := seedPublishedPricingRuntime(t, cfg)
	closePricingStartupPools(t, seed, seed)

	application := newInitializedApp(t, cfg)
	var state entities.PricingMigrationState
	if err := application.DB.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.InitKind != "legacy" || !state.SchemaComplete || !state.DataComplete || state.BackupPath == nil || state.BaselineJSON == nil {
		t.Fatalf("legacy completion lacked protected backup or data: %+v", state)
	}
	if _, err := os.Stat(*state.BackupPath); err != nil {
		t.Fatalf("protected old backup is unavailable: %v", err)
	}
	var stored entities.UsageEvent
	if err := application.DB.Where("event_key = ?", "old-usage").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.InputTokens != 100 || stored.CostUSD == nil || stored.CostAvailable == nil || !*stored.CostAvailable || *stored.CostUSD < 0.000199999 || *stored.CostUSD > 0.000200001 {
		t.Fatalf("legacy event facts or fee changed incorrectly: %+v", stored)
	}
	var hourly entities.UsageOverviewHourlyStat
	if err := application.DB.Take(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	if hourly.RequestCount != 1 || hourly.InputTokens != 100 || hourly.CostUSD == nil || *hourly.CostUSD < 0.000199999 || *hourly.CostUSD > 0.000200001 {
		t.Fatalf("legacy overview facts or fee changed incorrectly: %+v", hourly)
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := application.DB.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.LastAggregatedUsageEventID != stored.ID {
		t.Fatalf("overview checkpoint changed from event ID %d to %d", stored.ID, checkpoint.LastAggregatedUsageEventID)
	}
}

func closePricingStartupPools(t *testing.T, writer, reader *gorm.DB) {
	t.Helper()
	if reader != writer {
		if handle, err := reader.DB(); err == nil {
			if err := handle.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if handle, err := writer.DB(); err == nil {
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type pricingCloseWaiter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *pricingCloseWaiter) WaitPricingRecalculation() {
	close(w.entered)
	<-w.release
}

// TestAppCloseWaitsForPricingTaskBeforeDatabase 验证关停先排空重算，再关闭其仍在使用的 SQLite 连接。
func TestAppCloseWaitsForPricingTaskBeforeDatabase(t *testing.T) {
	cfg := testAppConfig(t)
	db, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	waiter := &pricingCloseWaiter{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(waiter.release) }) })
	application := &keeperapp.App{DB: db, PricingService: waiter}
	done := make(chan error, 1)
	go func() { done <- application.Close() }()
	awaitAppSignal(t, waiter.entered, "App.Close did not wait for pricing task")
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("database closed while pricing task still held lifecycle: %v", err)
	}
	once.Do(func() { close(waiter.release) })
	if err := awaitAppError(t, done, "App.Close did not finish after pricing task"); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("database remained open after pricing task and App.Close finished")
	}
}
