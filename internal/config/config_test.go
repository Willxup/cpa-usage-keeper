package config

import (
	"testing"
	"time"
)

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("SQLITE_PATH", "/tmp/app.db")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Fatalf("expected default app port 8080, got %s", cfg.AppPort)
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
	if cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("expected default request timeout 30s, got %s", cfg.RequestTimeout)
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

	t.Run("missing sqlite path", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "SQLITE_PATH is required" {
			t.Fatalf("expected SQLITE_PATH required error, got %v", err)
		}
	})

	t.Run("missing login password when auth enabled", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:8317")
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("SQLITE_PATH", "/tmp/app.db")
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
	t.Setenv("POLL_INTERVAL", "1m")
	t.Setenv("BACKUP_ENABLED", "false")
	t.Setenv("BACKUP_DIR", "/tmp/backups")
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

	if cfg.AppPort != "9090" || cfg.PollInterval != time.Minute || cfg.BackupEnabled || cfg.BackupDir != "/tmp/backups" || cfg.BackupRetentionDays != 7 || cfg.RequestTimeout != 15*time.Second || cfg.LogLevel != "debug" || !cfg.AuthEnabled || cfg.LoginPassword != "top-secret" || cfg.AuthSessionTTL != 12*time.Hour {
		t.Fatalf("unexpected config override result: %+v", cfg)
	}
}
