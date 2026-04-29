package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
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
}

func TestNewWithConfigSelectsRedisDrain(t *testing.T) {
	for _, mode := range []string{"redis", "auto"} {
		t.Run(mode, func(t *testing.T) {
			app, err := NewWithConfig(testAppConfig(t, mode))
			if err != nil {
				t.Fatalf("NewWithConfig returned error: %v", err)
			}
			defer app.Close()
			if _, ok := app.Poller.(*poller.RedisDrain); !ok {
				t.Fatalf("expected %s to use redis drain, got %T", mode, app.Poller)
			}
		})
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
	if !strings.Contains(content, "msg=\"usage sync mode selected\"") || !strings.Contains(content, "mode=redis") {
		t.Fatalf("expected configured usage sync mode log, got %q", content)
	}
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
