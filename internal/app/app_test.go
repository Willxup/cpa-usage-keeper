package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
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
		t.Fatal("expected poller to be initialized")
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
		t.Fatalf("expected redis to use redis drain, got %T", app.Poller)
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
		t.Fatal("expected sync poller to be initialized")
	}
	if app.Maintenance == nil {
		t.Fatal("expected independent maintenance runner to be initialized")
	}
}

func TestRunStartsPollerAndMaintenanceIndependently(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	pollerStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	metadataStarted := make(chan struct{})
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
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	backupRunner.sleep = func(context.Context, time.Duration) bool {
		close(backupStarted)
		return false
	}
	app := &App{
		Config:            &cfg,
		Router:            gin.New(),
		Poller:            &appRunStub{started: pollerStarted},
		Maintenance:       maintenance,
		MetadataSync:      metadataRunner,
		BackupMaintenance: backupRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-pollerStarted:
	case <-time.After(time.Second):
		t.Fatal("expected poller runner to start")
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
