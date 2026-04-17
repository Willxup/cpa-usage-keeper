package app

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
)

func TestNewWithConfigBuildsPollerAndRouter(t *testing.T) {
	app, err := NewWithConfig(config.Config{
		AppPort:             "8080",
		CPABaseURL:          "https://cpa.example.com",
		CPAManagementKey:    "secret",
		PollInterval:        time.Minute,
		SQLitePath:          t.TempDir() + "/app.db",
		BackupEnabled:       true,
		BackupDir:           t.TempDir() + "/backups",
		BackupRetentionDays: 30,
		RequestTimeout:      5 * time.Second,
		LogLevel:            "info",
	})
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
