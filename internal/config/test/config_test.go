package test

import (
	. "cpa-usage-keeper/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/cpa"
)

var configEnvKeys = []string{
	"APP_HOST", "APP_PORT", "APP_BASE_PATH", "CPA_PUBLIC_URL", "WORK_DIR", "CPA_BASE_URL", "CPA_MANAGEMENT_KEY", "POLL_INTERVAL",
	"USAGE_SYNC_MODE", "REDIS_QUEUE_ADDR", "REDIS_QUEUE_TLS", "REDIS_QUEUE_BATCH_SIZE", "REDIS_QUEUE_IDLE_INTERVAL",
	"USAGE_RAW_RETENTION_DAYS", "SQLITE_PATH", "BACKUP_ENABLED", "BACKUP_DIR", "BACKUP_INTERVAL", "BACKUP_RETENTION_DAYS",
	"REQUEST_TIMEOUT", "LOG_LEVEL", "LOG_FILE_ENABLED", "LOG_DIR", "LOG_RETENTION_DAYS",
	"AUTH_ENABLED", "LOGIN_PASSWORD", "AUTH_SESSION_TTL", "TRUSTED_PROXY_CIDRS", "TZ", "TLS_SKIP_VERIFY", "QUOTA_REFRESH_WORKER_LIMIT", "QUOTA_UPSTREAM_RESPONSES_ENABLED",
	"API_KEY_VIEWER_LOCAL_RANKING_ENABLED",
}

func TestMain(m *testing.M) {
	for _, key := range configEnvKeys {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
	if err := os.Setenv("LOGIN_PASSWORD", "test-login-password"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func withIsolatedEnvFiles(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Setenv("LOGIN_PASSWORD", "test-login-password")
	exeDir := t.TempDir()
	previousExecutableDir := executableDir
	t.Cleanup(func() { executableDir = previousExecutableDir })
	t.Chdir(t.TempDir())
	executableDir = func() (string, error) { return exeDir, nil }
}

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
	if cfg.CPAPublicURL != "" {
		t.Fatalf("expected default CPA public URL to be empty, got %q", cfg.CPAPublicURL)
	}
	if !cfg.BackupEnabled {
		t.Fatal("expected backup to be enabled by default")
	}
	if cfg.WorkDir != filepath.Join(".", "data") {
		t.Fatalf("expected default work dir ./data, got %s", cfg.WorkDir)
	}
	if cfg.BackupDir != filepath.Join("data", "backups") {
		t.Fatalf("expected default backup dir data/backups, got %s", cfg.BackupDir)
	}
	if cfg.BackupInterval != 24*time.Hour {
		t.Fatalf("expected default backup interval 24h, got %s", cfg.BackupInterval)
	}
	if cfg.BackupRetentionDays != 7 {
		t.Fatalf("expected default backup retention 7 days, got %d", cfg.BackupRetentionDays)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("expected default request timeout 30s, got %s", cfg.RequestTimeout)
	}
	if cfg.SQLitePath != filepath.Join("data", "app.db") {
		t.Fatalf("expected default sqlite path data/app.db, got %s", cfg.SQLitePath)
	}
	if !cfg.AuthEnabled {
		t.Fatal("expected auth to be enabled by default")
	}
	if cfg.AuthSessionTTL != 7*24*time.Hour {
		t.Fatalf("expected default auth session ttl 168h, got %s", cfg.AuthSessionTTL)
	}
	if cfg.TLSSkipVerify {
		t.Fatal("expected TLS skip verify to be disabled by default")
	}
	if cfg.RedisQueueTLS {
		t.Fatal("expected redis queue TLS to be disabled by default")
	}
	if cfg.RedisQueueAddr != "" {
		t.Fatalf("expected default redis queue addr to be empty, got %q", cfg.RedisQueueAddr)
	}
	if cfg.RedisQueueBatchSize != 10000 {
		t.Fatalf("expected default redis queue batch size 10000, got %d", cfg.RedisQueueBatchSize)
	}
	if cfg.RedisQueueIdleInterval != time.Second {
		t.Fatalf("expected default redis queue idle interval 1s, got %s", cfg.RedisQueueIdleInterval)
	}
	if cfg.MetadataSyncInterval != MetadataSyncIntervalDefault {
		t.Fatalf("expected default metadata sync interval 30s, got %s", cfg.MetadataSyncInterval)
	}
	if cfg.QuotaRefreshWorkerLimit != 10 {
		t.Fatalf("expected default quota refresh worker limit 10, got %d", cfg.QuotaRefreshWorkerLimit)
	}
	if !cfg.LogFileEnabled {
		t.Fatal("expected log file output to be enabled by default")
	}
	if cfg.LogDir != filepath.Join("data", "logs") {
		t.Fatalf("expected default log dir data/logs, got %s", cfg.LogDir)
	}
	if cfg.LogRetentionDays != 7 {
		t.Fatalf("expected default log retention 7 days, got %d", cfg.LogRetentionDays)
	}
}

func TestLoadReadsSpecifiedEnvFile(t *testing.T) {
	withIsolatedEnvFiles(t)
	envDir := t.TempDir()
	envPath := filepath.Join(envDir, "custom.env")
	if err := os.WriteFile(envPath, []byte("CPA_BASE_URL=https://from-file.example.com\nCPA_MANAGEMENT_KEY=from-file\nAPP_PORT=9091\nWORK_DIR=./custom-data\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: envPath})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.CPABaseURL != "https://from-file.example.com" || cfg.CPAManagementKey != "from-file" || cfg.AppPort != "9091" || cfg.WorkDir != filepath.Join(envDir, "custom-data") || cfg.SQLitePath != filepath.Join(envDir, "custom-data", "app.db") || cfg.LogDir != filepath.Join(envDir, "custom-data", "logs") || cfg.BackupDir != filepath.Join(envDir, "custom-data", "backups") {
		t.Fatalf("expected config values from specified env file, got %+v", cfg)
	}
}

func TestLoadResolvesRelativeEnvFilePathBase(t *testing.T) {
	withIsolatedEnvFiles(t)
	if err := os.Mkdir("config", 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join("config", "app.env"), []byte("CPA_BASE_URL=https://relative-env.example.com\nCPA_MANAGEMENT_KEY=relative\nWORK_DIR=./data\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: filepath.Join("config", "app.env")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	envFileAbsolutePath, err := filepath.Abs(filepath.Join("config", "app.env"))
	if err != nil {
		t.Fatalf("resolve env file path: %v", err)
	}
	expectedWorkDir := filepath.Join(filepath.Dir(envFileAbsolutePath), "data")
	if cfg.WorkDir != expectedWorkDir || cfg.SQLitePath != filepath.Join(expectedWorkDir, "app.db") || cfg.LogDir != filepath.Join(expectedWorkDir, "logs") || cfg.BackupDir != filepath.Join(expectedWorkDir, "backups") {
		t.Fatalf("expected paths under %q, got %+v", expectedWorkDir, cfg)
	}
}

func TestLoadIgnoresLegacyPathOverrides(t *testing.T) {
	withIsolatedEnvFiles(t)
	envDir := t.TempDir()
	envPath := filepath.Join(envDir, "legacy.env")
	content := "CPA_BASE_URL=https://legacy.example.com\nCPA_MANAGEMENT_KEY=legacy\nWORK_DIR=./work\nSQLITE_PATH=./legacy/app.db\nLOG_DIR=./legacy/logs\nBACKUP_DIR=./legacy/backups\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load(LoadOptions{EnvFile: envPath})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	expectedWorkDir := filepath.Join(envDir, "work")
	if cfg.WorkDir != expectedWorkDir || cfg.SQLitePath != filepath.Join(expectedWorkDir, "app.db") || cfg.LogDir != filepath.Join(expectedWorkDir, "logs") || cfg.BackupDir != filepath.Join(expectedWorkDir, "backups") {
		t.Fatalf("expected legacy path overrides to be ignored, got %+v", cfg)
	}
}

func TestLoadRejectsMissingSpecifiedEnvFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.env")

	_, err := Load(LoadOptions{EnvFile: missingPath})
	if err == nil || !strings.Contains(err.Error(), "stat env file") {
		t.Fatalf("expected missing specified env file error, got %v", err)
	}
}

func TestLoadFallsBackToExecutableDirEnv(t *testing.T) {
	withIsolatedEnvFiles(t)
	exeDir, err := executableDir()
	if err != nil {
		t.Fatalf("get executable dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exeDir, ".env"), []byte("CPA_BASE_URL=https://from-exe.example.com\nCPA_MANAGEMENT_KEY=from-exe\nWORK_DIR=./data\n"), 0o600); err != nil {
		t.Fatalf("write executable env file: %v", err)
	}

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.CPABaseURL != "https://from-exe.example.com" || cfg.CPAManagementKey != "from-exe" || cfg.WorkDir != filepath.Join(exeDir, "data") || cfg.SQLitePath != filepath.Join(exeDir, "data", "app.db") || cfg.LogDir != filepath.Join(exeDir, "data", "logs") || cfg.BackupDir != filepath.Join(exeDir, "data", "backups") {
		t.Fatalf("expected config values from executable dir env, got %+v", cfg)
	}
}

func TestLoadFromEnvRequiresCriticalValues(t *testing.T) {
	withIsolatedEnvFiles(t)

	t.Run("missing base url", func(t *testing.T) {
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_BASE_URL is required" {
			t.Fatalf("expected CPA_BASE_URL required error, got %v", err)
		}
	})

	t.Run("missing management key", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "CPA_MANAGEMENT_KEY is required" {
			t.Fatalf("expected CPA_MANAGEMENT_KEY required error, got %v", err)
		}
	})

	t.Run("missing login password when auth enabled", func(t *testing.T) {
		t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
		t.Setenv("CPA_MANAGEMENT_KEY", "secret")
		t.Setenv("AUTH_ENABLED", "true")
		t.Setenv("LOGIN_PASSWORD", "")

		_, err := LoadFromEnv()
		if err == nil || err.Error() != "LOGIN_PASSWORD is required when AUTH_ENABLED is true" {
			t.Fatalf("expected LOGIN_PASSWORD required error, got %v", err)
		}
	})
}

func TestLoadFromEnvIgnoresRemovedLegacySyncEnvVars(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("USAGE_SYNC_MODE", "invalid")
	t.Setenv("POLL_INTERVAL", "not-a-duration")

	_, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv should ignore removed legacy sync env vars, got error: %v", err)
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

func TestLoadFromEnvParsesOverrides(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("WORK_DIR", "/tmp/work")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_BASE_PATH", "/cpa/")
	t.Setenv("CPA_PUBLIC_URL", "https://cpa.public.example.com/")
	t.Setenv("BACKUP_ENABLED", "false")
	t.Setenv("BACKUP_INTERVAL", "2h")
	t.Setenv("BACKUP_RETENTION_DAYS", "7")
	t.Setenv("REQUEST_TIMEOUT", "15s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FILE_ENABLED", "false")
	t.Setenv("LOG_RETENTION_DAYS", "14")
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("LOGIN_PASSWORD", "top-secret")
	t.Setenv("AUTH_SESSION_TTL", "12h")
	t.Setenv("REDIS_QUEUE_IDLE_INTERVAL", "2s")
	t.Setenv("TLS_SKIP_VERIFY", "true")
	t.Setenv("REDIS_QUEUE_TLS", "true")
	t.Setenv("QUOTA_REFRESH_WORKER_LIMIT", "8")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if !cfg.TLSSkipVerify {
		t.Fatal("expected TLS skip verify to be enabled when set to true")
	}
	if !cfg.RedisQueueTLS {
		t.Fatal("expected redis queue TLS to be enabled when set to true")
	}
	if cfg.AppPort != "9090" || cfg.AppBasePath != "/cpa" || cfg.CPAPublicURL != "https://cpa.public.example.com/" || cfg.WorkDir != "/tmp/work" || cfg.SQLitePath != filepath.Join("/tmp/work", "app.db") || cfg.BackupEnabled || cfg.BackupDir != filepath.Join("/tmp/work", "backups") || cfg.BackupInterval != 2*time.Hour || cfg.BackupRetentionDays != 7 || cfg.RequestTimeout != 15*time.Second || cfg.LogLevel != "debug" || cfg.LogFileEnabled || cfg.LogDir != filepath.Join("/tmp/work", "logs") || cfg.LogRetentionDays != 14 || !cfg.AuthEnabled || cfg.LoginPassword != "top-secret" || cfg.AuthSessionTTL != 12*time.Hour || cfg.RedisQueueIdleInterval != 2*time.Second || cfg.QuotaRefreshWorkerLimit != 8 {
		t.Fatalf("unexpected config override result: %+v", cfg)
	}
}

func TestLoadFromEnvIgnoresRemovedMetadataSyncIntervalOverride(t *testing.T) {
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("REDIS_METADATA_SYNC_INTERVAL", "45s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.MetadataSyncInterval != MetadataSyncIntervalDefault {
		t.Fatalf("expected removed env overrides to be ignored, got metadata_interval=%s", cfg.MetadataSyncInterval)
	}
}

// 保留可执行文件目录回退测试的隔离夹具，不改变生产加载路径。
//
//go:linkname executableDir cpa-usage-keeper/internal/config.executableDir
var executableDir func() (string, error)
