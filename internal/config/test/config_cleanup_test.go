package test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
)

var isolatedConfigEnvKeys = []string{
	"APP_HOST", "APP_PORT", "APP_BASE_PATH", "CPA_PUBLIC_URL", "WORK_DIR", "CPA_BASE_URL", "CPA_MANAGEMENT_KEY",
	"CPA_REQUEST_LOG_ACCESS_ENABLED", "API_KEY_VIEWER_LOCAL_RANKING_ENABLED",
	"REDIS_QUEUE_ADDR", "REDIS_QUEUE_TLS", "REDIS_QUEUE_BATCH_SIZE", "REDIS_QUEUE_IDLE_INTERVAL",
	"BACKUP_ENABLED", "BACKUP_INTERVAL", "BACKUP_RETENTION_DAYS",
	"REQUEST_TIMEOUT", "LOG_LEVEL", "LOG_FILE_ENABLED", "LOG_DIR", "LOG_RETENTION_DAYS",
	"AUTH_ENABLED", "LOGIN_PASSWORD", "AUTH_SESSION_TTL", "TRUSTED_PROXY_CIDRS", "TZ", "TLS_ENABLED", "TLS_CERT_FILE", "TLS_KEY_FILE",
	"TLS_SKIP_VERIFY", "QUOTA_REFRESH_WORKER_LIMIT", "QUOTA_UPSTREAM_RESPONSES_ENABLED",
}

func TestLoadOptionalAccessFlags(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value func(*config.Config) bool
	}{
		{"CPA_REQUEST_LOG_ACCESS_ENABLED", func(cfg *config.Config) bool { return cfg.CPARequestLogAccessEnabled }},
		{"QUOTA_UPSTREAM_RESPONSES_ENABLED", func(cfg *config.Config) bool { return cfg.QuotaUpstreamResponsesEnabled }},
		{"API_KEY_VIEWER_LOCAL_RANKING_ENABLED", func(cfg *config.Config) bool { return cfg.APIKeyViewerLocalRankingEnabled }},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", tc.key, enabled), func(t *testing.T) {
				isolateConfigEnv(t)
				setRequiredConfig(t)
				if enabled {
					t.Setenv(tc.key, "true")
				}
				cfg, err := config.LoadFromEnv()
				if err != nil {
					t.Fatalf("LoadFromEnv: %v", err)
				}
				if got := tc.value(cfg); got != enabled {
					t.Fatalf("flag = %t, want %t", got, enabled)
				}
			})
		}
	}
}

func isolateConfigEnv(t *testing.T) {
	t.Helper()
	previousLocal := time.Local
	t.Cleanup(func() { time.Local = previousLocal })
	for _, key := range isolatedConfigEnvKeys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Setenv("LOGIN_PASSWORD", "test-login-password")
}
