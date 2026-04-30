package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestNewWithConfigBuildsPollerAndRouter(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t, "legacy_export"))
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
}

func TestNewWithConfigSelectsLegacyPoller(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t, "legacy_export"))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if _, ok := app.Poller.(*poller.Poller); !ok {
		t.Fatalf("expected legacy_export to use interval poller, got %T", app.Poller)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestNewWithConfigSelectsRedisDrain(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t, "redis"))
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

func TestNewWithConfigRunsTemporaryStartupSnapshotCleanup(t *testing.T) {
	cfg := testAppConfig(t, "legacy_export")
	seedDB, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	oldRun, err := repository.CreateSnapshotRun(seedDB, repository.SnapshotRunInput{FetchedAt: time.Now().AddDate(0, 0, -8), RawPayload: []byte(`old`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun old returned error: %v", err)
	}
	latestRun, err := repository.CreateSnapshotRun(seedDB, repository.SnapshotRunInput{FetchedAt: time.Now(), RawPayload: []byte(`latest`)})
	if err != nil {
		t.Fatalf("CreateSnapshotRun latest returned error: %v", err)
	}
	sqlDB, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close seed sql db: %v", err)
	}

	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	var remaining []models.SnapshotRun
	if err := app.DB.Order("id asc").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining snapshot runs: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != latestRun.ID {
		t.Fatalf("expected startup cleanup to keep only latest snapshot %d and delete %d, got %+v", latestRun.ID, oldRun.ID, remaining)
	}
}

func TestNewWithConfigCreatesIndependentMaintenanceRunner(t *testing.T) {
	for _, mode := range []string{"redis", "legacy_export"} {
		t.Run(mode, func(t *testing.T) {
			app, err := NewWithConfig(testAppConfig(t, mode))
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
		})
	}
}

func TestNewWithConfigAutoUsesRedisDrainWhenStartupProbeSucceeds(t *testing.T) {
	probeCalls := 0
	withRedisStartupProbe(t, func(context.Context, config.Config) error {
		probeCalls++
		return nil
	})

	app, err := NewWithConfig(testAppConfig(t, "auto"))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	if probeCalls != 1 {
		t.Fatalf("expected one startup probe, got %d", probeCalls)
	}
	if app.Config.UsageSyncMode != "redis" {
		t.Fatalf("expected effective mode redis, got %q", app.Config.UsageSyncMode)
	}
	if _, ok := app.Poller.(*poller.RedisDrain); !ok {
		t.Fatalf("expected auto with successful probe to use redis drain, got %T", app.Poller)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestNewWithConfigAutoUsesLegacyPollerWhenStartupProbeFails(t *testing.T) {
	probeCalls := 0
	withRedisStartupProbe(t, func(context.Context, config.Config) error {
		probeCalls++
		return errors.New("redis unavailable")
	})

	app, err := NewWithConfig(testAppConfig(t, "auto"))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	if probeCalls != 1 {
		t.Fatalf("expected one startup probe, got %d", probeCalls)
	}
	if app.Config.UsageSyncMode != "legacy_export" {
		t.Fatalf("expected effective mode legacy_export, got %q", app.Config.UsageSyncMode)
	}
	if app.Config.PollInterval != time.Minute {
		t.Fatalf("expected auto resolved legacy poller to keep configured poll interval, got %s", app.Config.PollInterval)
	}
	if _, ok := app.Poller.(*poller.Poller); !ok {
		t.Fatalf("expected auto with failed probe to use legacy poller, got %T", app.Poller)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestResolveUsageSyncModeLogsEffectiveMode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probeErr  error
		effective string
	}{
		{name: "redis", effective: "redis"},
		{name: "legacy_export", probeErr: errors.New("redis unavailable"), effective: "legacy_export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureAppInfoLogs(t)
			withRedisStartupProbe(t, func(context.Context, config.Config) error {
				return tc.probeErr
			})

			cfg := testAppConfig(t, "auto")
			resolved := resolveUsageSyncMode(context.Background(), cfg)
			if resolved.UsageSyncMode != tc.effective {
				t.Fatalf("expected effective mode %q, got %q", tc.effective, resolved.UsageSyncMode)
			}
			if resolved.PollInterval != cfg.PollInterval {
				t.Fatalf("expected poll interval to remain %s, got %s", cfg.PollInterval, resolved.PollInterval)
			}
			content := logs.String()
			for _, expected := range []string{"level=info", "msg=\"usage sync auto mode resolved\"", "configured_mode=auto", "effective_mode=" + tc.effective} {
				if !strings.Contains(content, expected) {
					t.Fatalf("expected auto resolution log to contain %q, got %q", expected, content)
				}
			}
		})
	}
}

func TestNewWithConfigDoesNotProbeForExplicitModes(t *testing.T) {
	withRedisStartupProbe(t, func(context.Context, config.Config) error {
		t.Fatal("unexpected startup probe")
		return nil
	})

	for _, mode := range []string{"redis", "legacy_export"} {
		t.Run(mode, func(t *testing.T) {
			app, err := NewWithConfig(testAppConfig(t, mode))
			if err != nil {
				t.Fatalf("NewWithConfig returned error: %v", err)
			}
			defer app.Close()
			if app.Config.UsageSyncMode != mode {
				t.Fatalf("expected mode %q to remain unchanged, got %q", mode, app.Config.UsageSyncMode)
			}
		})
	}
}

func TestTemporaryStartupSnapshotCleanupLogsStartAndSuccess(t *testing.T) {
	logs := captureAppInfoLogs(t)
	db, err := repository.OpenDatabase(testAppConfig(t, "legacy_export"))
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	if err := runTemporaryStartupSnapshotRunsCleanup(db); err != nil {
		t.Fatalf("runTemporaryStartupSnapshotRunsCleanup returned error: %v", err)
	}

	content := logs.String()
	for _, expected := range []string{"level=info", "msg=\"temporary snapshot runs cleanup started\"", "msg=\"temporary snapshot runs cleanup completed\""} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected temporary cleanup log to contain %q, got %q", expected, content)
		}
	}
}

func TestTemporaryStartupSnapshotCleanupLogsFailure(t *testing.T) {
	logs := captureAppInfoLogs(t)

	if err := runTemporaryStartupSnapshotRunsCleanup(nil); err == nil {
		t.Fatal("expected runTemporaryStartupSnapshotRunsCleanup to return an error")
	}

	content := logs.String()
	for _, expected := range []string{"level=error", "msg=\"temporary snapshot runs cleanup failed\""} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected temporary cleanup error log to contain %q, got %q", expected, content)
		}
	}
}

func TestRunStartsPollerAndMaintenanceIndependently(t *testing.T) {
	cfg := testAppConfig(t, "redis")
	cfg.AppPort = "invalid-port"
	pollerStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	maintenance := NewStorageCleanupRunner(&maintenanceSyncStub{})
	maintenance.sleep = func(context.Context, time.Duration) bool {
		close(maintenanceStarted)
		return false
	}
	app := &App{
		Config:      &cfg,
		Router:      gin.New(),
		Poller:      &appRunStub{started: pollerStarted},
		Maintenance: maintenance,
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
}

func TestRunLogsConfiguredUsageSyncMode(t *testing.T) {
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

	cfg := testAppConfig(t, "redis")
	cfg.AppPort = "invalid-port"
	app := &App{
		Config: &cfg,
		Router: gin.New(),
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}

	content := logs.String()
	for _, expected := range []string{"msg=\"usage sync mode selected\"", "configured_mode=redis", "effective_mode=redis"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected usage sync mode log to contain %q, got %q", expected, content)
		}
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

func withRedisStartupProbe(t *testing.T, probe func(context.Context, config.Config) error) {
	t.Helper()
	previous := redisStartupProbe
	redisStartupProbe = probe
	t.Cleanup(func() { redisStartupProbe = previous })
}

func testAppConfig(t *testing.T, syncMode string) config.Config {
	t.Helper()
	return config.Config{
		AppPort:                   "8080",
		CPABaseURL:                "https://cpa.example.com",
		CPAManagementKey:          "secret",
		UsageSyncMode:             syncMode,
		PollInterval:              time.Minute,
		RedisQueueIdleInterval:    time.Second,
		RedisQueueErrorBackoff:    10 * time.Second,
		RedisMetadataSyncInterval: 30 * time.Second,
		SQLitePath:                t.TempDir() + "/app.db",
		BackupEnabled:             true,
		BackupDir:                 t.TempDir() + "/backups",
		BackupRetentionDays:       30,
		RequestTimeout:            5 * time.Second,
		LogLevel:                  "info",
		LogFileEnabled:            false,
		LogRetentionDays:          7,
	}
}
