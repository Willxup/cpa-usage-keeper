package app

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
)

func TestNewWithConfigBuildsPollerAndRouter(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t, "legacy_export"))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if app.Poller == nil {
		t.Fatal("expected poller to be initialized")
	}
	if app.Router == nil {
		t.Fatal("expected router to be initialized")
	}
}

func TestNewWithConfigSelectsLegacyPoller(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t, "legacy_export"))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
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
			if _, ok := app.Poller.(*poller.RedisDrain); !ok {
				t.Fatalf("expected %s to use redis drain, got %T", mode, app.Poller)
			}
		})
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
	}
}
