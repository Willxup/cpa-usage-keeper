package config

import (
	"testing"
	"time"
)

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Fatalf("expected default app port 8080, got %s", cfg.AppPort)
	}
	if cfg.AppBasePath != "" {
		t.Fatalf("expected default app base path to be empty, got %q", cfg.AppBasePath)
	}
	if cfg.PollInterval != 5*time.Minute {
		t.Fatalf("expected default poll interval 5m, got %s", cfg.PollInterval)
	}
	if !cfg.BackupEnabled {
		t.Fatal("expected backup to be enabled by default")
	}
	if cfg.BackupDir != "/data/backups" {
		t.Fatalf("expected default backup dir /data/backups, got %s", cfg.BackupDir)
	}
	if cfg.BackupInterval != time.Hour {
		t.Fatalf("expected default backup interval 1h, got %s", cfg.BackupInterval)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("expected default request timeout 30s, got %s", cfg.RequestTimeout)
	}
	if cfg.SQLitePath != "/data/app.db" {
		t.Fatalf("expected default sqlite path /data/app.db, got %s", cfg.SQLitePath)
	}
	if cfg.AuthEnabled {
		t.Fatal("expected auth to be disabled by default")
	}
	if cfg.AuthSessionTTL != 7*24*time.Hour {
		t.Fatalf("expected default auth session ttl 168h, got %s", cfg.AuthSessionTTL)
	}
}

func TestLoadFromEnvRequiresCriticalValues(t *testing.T) {
	t.Run("missing base url", func(t *testing.T) {
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("SQLITE_PATH", "/tmp/app.db")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_BASE_URL is required" {
			t.Fatalf("expected CPA_BASE_URL required error, got %v", err)
		}
	})

	t.Run("missing management key", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
		t.Setenv("SQLITE_PATH", "/tmp/app.db")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_MANAGEMENT_KEY is required" {
			t.Fatalf("expected CPA_MANAGEMENT_KEY required error, got %v", err)
		}
	})

	t.Run("missing login password when auth enabled", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("AUTH_ENABLED", "true")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "LOGIN_PASSWORD is required when AUTH_ENABLED is true" {
			t.Fatalf("expected LOGIN_PASSWORD required error, got %v", err)
		}
	})
}

func TestLoadFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("SQLITE_PATH", "/tmp/app.db")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_BASE_PATH", "/cpa/")
	t.Setenv("POLL_INTERVAL", "1m")
	t.Setenv("BACKUP_ENABLED", "false")
	t.Setenv("BACKUP_DIR", "/tmp/backups")
	t.Setenv("BACKUP_INTERVAL", "2h")
	t.Setenv("BACKUP_RETENTION_DAYS", "7")
	t.Setenv("REQUEST_TIMEOUT", "15s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("LOGIN_PASSWORD", "top-secret")
	t.Setenv("AUTH_SESSION_TTL", "12h")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.AppPort != "9090" || cfg.AppBasePath != "/cpa" || cfg.PollInterval != time.Minute || cfg.BackupEnabled || cfg.BackupDir != "/tmp/backups" || cfg.BackupInterval != 2*time.Hour || cfg.BackupRetentionDays != 7 || cfg.RequestTimeout != 15*time.Second || cfg.LogLevel != "debug" || !cfg.AuthEnabled || cfg.LoginPassword != "top-secret" || cfg.AuthSessionTTL != 12*time.Hour {
		t.Fatalf("unexpected config override result: %+v", cfg)
	}
}

func TestLoadFromEnvRejectsInvalidBasePath(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("SQLITE_PATH", "/tmp/app.db")
	t.Setenv("APP_BASE_PATH", "cpa")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "APP_BASE_PATH is invalid: must start with '/'" {
		t.Fatalf("expected APP_BASE_PATH validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveAuthSessionTTL(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("AUTH_SESSION_TTL", "0s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "AUTH_SESSION_TTL must be positive" {
		t.Fatalf("expected AUTH_SESSION_TTL validation error, got %v", err)
	}
}
