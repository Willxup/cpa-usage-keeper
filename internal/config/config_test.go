package config

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
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
	if cfg.UsageSyncMode != "auto" {
		t.Fatalf("expected default usage sync mode auto, got %s", cfg.UsageSyncMode)
	}
	if cfg.RedisQueueAddr != "" {
		t.Fatalf("expected default redis queue addr to be empty, got %q", cfg.RedisQueueAddr)
	}
	if cfg.RedisQueueKey != RedisQueueKeyDefault {
		t.Fatalf("expected default redis queue key queue, got %s", cfg.RedisQueueKey)
	}
	if cfg.RedisQueueBatchSize != 1000 {
		t.Fatalf("expected default redis queue batch size 1000, got %d", cfg.RedisQueueBatchSize)
	}
	if cfg.PollInterval != 5*time.Minute {
		t.Fatalf("expected default legacy export poll interval 5m, got %s", cfg.PollInterval)
	}
	if cfg.RedisQueueIdleInterval != time.Second {
		t.Fatalf("expected default redis queue idle interval 1s, got %s", cfg.RedisQueueIdleInterval)
	}
	if cfg.RedisQueueErrorBackoff != RedisQueueErrorBackoffDefault {
		t.Fatalf("expected default redis queue error backoff 10s, got %s", cfg.RedisQueueErrorBackoff)
	}
	if cfg.RedisMetadataSyncInterval != RedisMetadataSyncIntervalDefault {
		t.Fatalf("expected default redis metadata sync interval 30s, got %s", cfg.RedisMetadataSyncInterval)
	}
	if !cfg.LogFileEnabled {
		t.Fatal("expected log file output to be enabled by default")
	}
	if cfg.LogDir != "/data/logs" {
		t.Fatalf("expected default log dir /data/logs, got %s", cfg.LogDir)
	}
	if cfg.LogRetentionDays != 7 {
		t.Fatalf("expected default log retention 7 days, got %d", cfg.LogRetentionDays)
	}
}

func TestLoadFromEnvAppliesDefaultTimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if time.Local.String() != "Asia/Shanghai" {
		t.Fatalf("expected default local timezone Asia/Shanghai, got %s", time.Local)
	}
}

func TestLoadFromEnvHonorsExplicitTimeZone(t *testing.T) {
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	t.Setenv("TZ", "UTC")
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if time.Local.String() != "UTC" {
		t.Fatalf("expected explicit local timezone UTC, got %s", time.Local)
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
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
		t.Setenv("SQLITE_PATH", "/tmp/app.db")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_MANAGEMENT_KEY is required" {
			t.Fatalf("expected CPA_MANAGEMENT_KEY required error, got %v", err)
		}
	})

	t.Run("missing login password when auth enabled", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("AUTH_ENABLED", "true")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "LOGIN_PASSWORD is required when AUTH_ENABLED is true" {
			t.Fatalf("expected LOGIN_PASSWORD required error, got %v", err)
		}
	})
}

func TestLoadFromEnvUsesLegacyExportPollDefault(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("USAGE_SYNC_MODE", "legacy_export")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.PollInterval != 5*time.Minute {
		t.Fatalf("expected legacy export poll interval 5m, got %s", cfg.PollInterval)
	}
}

func TestLoadFromEnvUsesExplicitPollIntervalOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("POLL_INTERVAL", "1m")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.PollInterval != time.Minute {
		t.Fatalf("expected explicit poll interval 1m, got %s", cfg.PollInterval)
	}
}

func TestLoadFromEnvUsesRedisQueueAddrOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "https://cpa.example.com")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_ADDR", "redis-stream.example.com:6380")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.RedisQueueAddr != "redis-stream.example.com:6380" {
		t.Fatalf("expected redis queue addr override, got %q", cfg.RedisQueueAddr)
	}
}

func TestLoadFromEnvIgnoresRemovedRedisQueueKeyOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "https://cpa.example.com")
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_KEY", "custom-queue")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.RedisQueueKey != RedisQueueKeyDefault {
		t.Fatalf("expected removed redis queue key override to be ignored, got %q", cfg.RedisQueueKey)
	}
}

func TestLoadFromEnvRejectsInvalidUsageSyncMode(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("USAGE_SYNC_MODE", "invalid")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "USAGE_SYNC_MODE must be one of auto, redis, legacy_export" {
		t.Fatalf("expected usage sync mode validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveRedisQueueBatchSize(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_BATCH_SIZE", "0")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "REDIS_QUEUE_BATCH_SIZE must be positive" {
		t.Fatalf("expected REDIS_QUEUE_BATCH_SIZE validation error, got %v", err)
	}
}

func TestLoadFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
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
	t.Setenv("LOG_FILE_ENABLED", "false")
	t.Setenv("LOG_DIR", "/tmp/custom-logs")
	t.Setenv("LOG_RETENTION_DAYS", "14")
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("LOGIN_PASSWORD", "top-secret")
	t.Setenv("AUTH_SESSION_TTL", "12h")
	t.Setenv("REDIS_QUEUE_IDLE_INTERVAL", "2s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.AppPort != "9090" || cfg.AppBasePath != "/cpa" || cfg.PollInterval != time.Minute || cfg.BackupEnabled || cfg.BackupDir != "/tmp/backups" || cfg.BackupInterval != 2*time.Hour || cfg.BackupRetentionDays != 7 || cfg.RequestTimeout != 15*time.Second || cfg.LogLevel != "debug" || cfg.LogFileEnabled || cfg.LogDir != "/tmp/custom-logs" || cfg.LogRetentionDays != 14 || !cfg.AuthEnabled || cfg.LoginPassword != "top-secret" || cfg.AuthSessionTTL != 12*time.Hour || cfg.RedisQueueIdleInterval != 2*time.Second {
		t.Fatalf("unexpected config override result: %+v", cfg)
	}
}

func TestLoadFromEnvRejectsNegativeLogRetentionDays(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("LOG_RETENTION_DAYS", "-1")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "LOG_RETENTION_DAYS must be non-negative" {
		t.Fatalf("expected LOG_RETENTION_DAYS validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveRedisQueueIdleInterval(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_IDLE_INTERVAL", "0s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "REDIS_QUEUE_IDLE_INTERVAL must be positive" {
		t.Fatalf("expected REDIS_QUEUE_IDLE_INTERVAL validation error, got %v", err)
	}
}

func TestLoadFromEnvIgnoresRemovedRedisDrainEnvOverrides(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_QUEUE_ERROR_BACKOFF", "20s")
	t.Setenv("REDIS_METADATA_SYNC_INTERVAL", "45s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.RedisQueueErrorBackoff != RedisQueueErrorBackoffDefault || cfg.RedisMetadataSyncInterval != RedisMetadataSyncIntervalDefault {
		t.Fatalf("expected removed env overrides to be ignored, got error_backoff=%s metadata_interval=%s", cfg.RedisQueueErrorBackoff, cfg.RedisMetadataSyncInterval)
	}
}

func TestLoadFromEnvRejectsInvalidBasePath(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("SQLITE_PATH", "/tmp/app.db")
	t.Setenv("APP_BASE_PATH", "cpa")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "APP_BASE_PATH is invalid: must start with '/'" {
		t.Fatalf("expected APP_BASE_PATH validation error, got %v", err)
	}
}

func TestLoadFromEnvRejectsNonPositiveAuthSessionTTL(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("AUTH_SESSION_TTL", "0s")

	_, err := LoadFromEnv()
	if err == nil || err.Error() != "AUTH_SESSION_TTL must be positive" {
		t.Fatalf("expected AUTH_SESSION_TTL validation error, got %v", err)
	}
}
