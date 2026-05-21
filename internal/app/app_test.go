package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestAppCloseClosesDatabase(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	sqlDB, err := app.DB.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database ping to fail after app close")
	}
}

func TestNewWithConfigBuildsRedisDrainAndRouter(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected poller status provider to be initialized")
	}
	if app.RedisPull == nil {
		t.Fatal("expected redis pull runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected redis process runner to be initialized")
	}
	if app.Router == nil {
		t.Fatal("expected router to be initialized")
	}
	if app.LogCloser == nil {
		t.Fatal("expected log closer to be initialized")
	}
	if app.BackupMaintenance == nil {
		t.Fatal("expected database backup runner to be initialized")
	}
	if app.MetadataSync == nil {
		t.Fatal("expected metadata sync runner to be initialized")
	}
	if app.UsageModels == nil {
		t.Fatal("expected usage model aggregation runner to be initialized")
	}
}

func TestNewWithConfigAggregatesExistingOverviewStatsBeforeRunnersStart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app-startup-overview-catchup.db")
	seedDB, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(seedDB, []entities.UsageEvent{
		{EventKey: "legacy-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), TotalTokens: 150},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	seedSQL, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed sql db: %v", err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	logDir := t.TempDir()

	cfg := testAppConfig(t)
	cfg.SQLitePath = dbPath
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := app.DB.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("load overview checkpoint returned error: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected startup catch-up to aggregate legacy usage events, got checkpoint %+v", checkpoint)
	}
	logContent := readAppLogFile(t, logDir)
	if !strings.Contains(logContent, "starting usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up start log, got %s", logContent)
	}
	if !strings.Contains(logContent, "completed usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up completion log, got %s", logContent)
	}
}

func TestNewWithConfigAggregatesExistingUsageModelsBeforeRunnersStart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app-startup-model-catchup.db")
	seedDB, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(seedDB, []entities.UsageEvent{
		{EventKey: "legacy-model-event", APIGroupKey: "provider-a", Model: "claude-sonnet", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), TotalTokens: 150},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	seedSQL, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed sql db: %v", err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	logDir := t.TempDir()

	cfg := testAppConfig(t)
	cfg.SQLitePath = dbPath
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := app.DB.Where("name = ?", "usage_models").First(&checkpoint).Error; err != nil {
		t.Fatalf("load usage model checkpoint returned error: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected startup catch-up to aggregate usage models, got checkpoint %+v", checkpoint)
	}
	var row entities.UsageModel
	if err := app.DB.Where("model = ? AND auth_type = ? AND auth_index = ?", "claude-sonnet", "apikey", "auth-a").First(&row).Error; err != nil {
		t.Fatalf("load usage model row returned error: %v", err)
	}
	logContent := readAppLogFile(t, logDir)
	if !strings.Contains(logContent, "starting usage model aggregation catch-up") {
		t.Fatalf("expected usage model catch-up start log, got %s", logContent)
	}
	if !strings.Contains(logContent, "completed usage model aggregation catch-up") {
		t.Fatalf("expected usage model catch-up completion log, got %s", logContent)
	}
}

func TestNewWithConfigSkipsBackupRunnerWhenDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.BackupEnabled = false
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.BackupMaintenance != nil {
		t.Fatal("expected database backup runner to be skipped when backups are disabled")
	}
}

func TestNewWithConfigSelectsRedisDrain(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if _, ok := app.Poller.(*poller.RedisDrain); !ok {
		t.Fatalf("expected redis status provider to use redis drain, got %T", app.Poller)
	}
	if _, ok := app.RedisPull.(*poller.RedisPullRunner); !ok {
		t.Fatalf("expected redis pull runner, got %T", app.RedisPull)
	}
	if _, ok := app.RedisProcess.(*poller.RedisProcessRunner); !ok {
		t.Fatalf("expected redis process runner, got %T", app.RedisProcess)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestNewWithConfigCreatesIndependentMaintenanceRunner(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected sync status provider to be initialized")
	}
	if app.RedisPull == nil {
		t.Fatal("expected independent redis pull runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected independent redis process runner to be initialized")
	}
	if app.Maintenance == nil {
		t.Fatal("expected independent maintenance runner to be initialized")
	}
}

func TestRunStartsPollerAndMaintenanceIndependently(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	pullStarted := make(chan struct{})
	processStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	metadataStarted := make(chan struct{})
	usageModelsStarted := make(chan struct{})
	backupStarted := make(chan struct{})
	maintenance := NewStorageCleanupRunner(&maintenanceSyncStub{})
	maintenance.sleep = func(context.Context, time.Duration) bool {
		close(maintenanceStarted)
		return false
	}
	metadataRunner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Second)
	metadataRunner.sleep = func(context.Context, time.Duration) bool {
		close(metadataStarted)
		return false
	}
	usageModelsRunner := &appRunStub{started: usageModelsStarted}
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	backupRunner.sleep = func(context.Context, time.Duration) bool {
		close(backupStarted)
		return false
	}
	statusProvider := &appRunStub{started: make(chan struct{})}
	app := &App{
		Config:            &cfg,
		Router:            gin.New(),
		Poller:            statusProvider,
		RedisPull:         &appRunStub{started: pullStarted},
		RedisProcess:      &appRunStub{started: processStarted},
		Maintenance:       maintenance,
		MetadataSync:      metadataRunner,
		UsageModels:       usageModelsRunner,
		BackupMaintenance: backupRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-pullStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis pull runner to start")
	}
	select {
	case <-processStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis process runner to start")
	}
	select {
	case <-statusProvider.started:
		t.Fatal("expected poller status provider not to be started as a background runner")
	default:
	}
	select {
	case <-maintenanceStarted:
	case <-time.After(time.Second):
		t.Fatal("expected maintenance runner to start")
	}
	select {
	case <-metadataStarted:
	case <-time.After(time.Second):
		t.Fatal("expected metadata sync runner to start")
	}
	select {
	case <-usageModelsStarted:
	case <-time.After(time.Second):
		t.Fatal("expected usage model aggregation runner to start")
	}
	select {
	case <-backupStarted:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner to start")
	}
}

func TestRunCancelsBackgroundTasksWhenRouterStops(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	backupStarted := make(chan struct{})
	backupCanceled := make(chan struct{})
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	backupRunner.sleep = func(ctx context.Context, _ time.Duration) bool {
		close(backupStarted)
		<-ctx.Done()
		close(backupCanceled)
		return false
	}
	app := &App{
		Config:            &cfg,
		Router:            gin.New(),
		BackupMaintenance: backupRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-backupStarted:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner to start")
	}
	select {
	case <-backupCanceled:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner context to be canceled")
	}
}

type appRunStub struct {
	started chan struct{}
}

func (s *appRunStub) Run(context.Context) error {
	close(s.started)
	return nil
}

func (s *appRunStub) Status() poller.Status {
	return poller.Status{}
}

func (s *appRunStub) SyncNow(context.Context) error {
	return nil
}

func captureAppInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func readAppLogFile(t *testing.T, logDir string) string {
	t.Helper()
	path := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app log file: %v", err)
	}
	return string(content)
}

func testAppConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		AppPort:                "8080",
		CPABaseURL:             "https://cpa.example.com",
		CPAManagementKey:       "secret",
		RedisQueueIdleInterval: time.Second,
		RedisQueueErrorBackoff: 10 * time.Second,
		MetadataSyncInterval:   30 * time.Second,
		SQLitePath:             t.TempDir() + "/app.db",
		BackupEnabled:          true,
		BackupDir:              t.TempDir() + "/backups",
		BackupRetentionDays:    7,
		RequestTimeout:         5 * time.Second,
		LogLevel:               "info",
		LogFileEnabled:         false,
		LogRetentionDays:       7,
	}
}
