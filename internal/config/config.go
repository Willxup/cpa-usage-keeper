package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	RedisQueueKeyDefault             = "queue"
	RedisQueueErrorBackoffDefault    = 10 * time.Second
	RedisMetadataSyncIntervalDefault = 30 * time.Second
)

type Config struct {
	// AppPort 是 Web 服务监听端口。
	AppPort string
	// AppBasePath 是 Web 服务部署子路径，空值表示根路径。
	AppBasePath string
	// CPABaseURL 是 CPA 服务基础地址。
	CPABaseURL string
	// CPAManagementKey 是访问 CPA 管理数据的密钥。
	CPAManagementKey string
	// PollInterval 是 legacy 拉取间隔，也是 auto 模式 fallback 的节流间隔。
	PollInterval time.Duration
	// UsageSyncMode 决定使用 auto、redis 或 legacy_export 同步模式。
	UsageSyncMode string
	// RedisQueueAddr 是 CPA management data stream 的 TCP 地址，空值时按 CPA_BASE_URL 推导。
	RedisQueueAddr string
	// RedisQueueKey 是 CPA usage 队列名。
	RedisQueueKey string
	// RedisQueueBatchSize 是单次 Redis LPOP 最多拉取的消息数。
	RedisQueueBatchSize int
	// RedisQueueIdleInterval 是 Redis 队列为空时的下一次检查间隔。
	RedisQueueIdleInterval time.Duration
	// RedisQueueErrorBackoff 是 Redis 临时错误后的固定退避间隔。
	RedisQueueErrorBackoff time.Duration
	// RedisMetadataSyncInterval 是 Redis drain 模式下 metadata 的固定刷新间隔。
	RedisMetadataSyncInterval time.Duration
	// SQLitePath 是 SQLite 数据库文件路径。
	SQLitePath string
	// BackupEnabled 控制是否保存原始 export 备份文件。
	BackupEnabled bool
	// BackupDir 是原始 export 备份目录。
	BackupDir string
	// BackupInterval 是两次备份写入之间的最小间隔。
	BackupInterval time.Duration
	// BackupRetentionDays 是备份文件保留天数。
	BackupRetentionDays int
	// RequestTimeout 是访问 CPA HTTP 和 Redis TCP 的超时时间。
	RequestTimeout time.Duration
	// LogLevel 是应用日志级别。
	LogLevel string
	// LogFileEnabled 控制是否写入持久化日志文件。
	LogFileEnabled bool
	// LogDir 是应用日志文件目录。
	LogDir string
	// LogRetentionDays 是日志保留天数，0 表示不自动清理。
	LogRetentionDays int
	// AuthEnabled 控制是否启用登录保护。
	AuthEnabled bool
	// LoginPassword 是启用登录保护时使用的登录密码。
	LoginPassword string
	// AuthSessionTTL 是登录 session 有效时长。
	AuthSessionTTL time.Duration
}

func LoadFromEnv() (*Config, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return nil, err
	}

	usageSyncMode := getString("USAGE_SYNC_MODE", "auto")
	if usageSyncMode != "auto" && usageSyncMode != "redis" && usageSyncMode != "legacy_export" {
		return nil, fmt.Errorf("USAGE_SYNC_MODE must be one of auto, redis, legacy_export")
	}

	pollIntervalDefault := 30 * time.Second
	if usageSyncMode == "legacy_export" {
		pollIntervalDefault = 5 * time.Minute
	}
	pollInterval, err := getDuration("POLL_INTERVAL", pollIntervalDefault)
	if err != nil {
		return nil, err
	}

	redisQueueBatchSize, err := getInt("REDIS_QUEUE_BATCH_SIZE", 1000)
	if err != nil {
		return nil, err
	}
	if redisQueueBatchSize <= 0 {
		return nil, fmt.Errorf("REDIS_QUEUE_BATCH_SIZE must be positive")
	}

	redisQueueIdleInterval, err := getDuration("REDIS_QUEUE_IDLE_INTERVAL", time.Second)
	if err != nil {
		return nil, err
	}
	if redisQueueIdleInterval <= 0 {
		return nil, fmt.Errorf("REDIS_QUEUE_IDLE_INTERVAL must be positive")
	}

	requestTimeout, err := getDuration("REQUEST_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}

	backupEnabled, err := getBool("BACKUP_ENABLED", true)
	if err != nil {
		return nil, err
	}

	backupInterval, err := getDuration("BACKUP_INTERVAL", time.Hour)
	if err != nil {
		return nil, err
	}

	backupRetentionDays, err := getInt("BACKUP_RETENTION_DAYS", 30)
	if err != nil {
		return nil, err
	}

	logFileEnabled, err := getBool("LOG_FILE_ENABLED", true)
	if err != nil {
		return nil, err
	}
	logRetentionDays, err := getInt("LOG_RETENTION_DAYS", 7)
	if err != nil {
		return nil, err
	}
	if logRetentionDays < 0 {
		return nil, fmt.Errorf("LOG_RETENTION_DAYS must be non-negative")
	}

	authSessionTTL, err := getDuration("AUTH_SESSION_TTL", 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if authSessionTTL <= 0 {
		return nil, fmt.Errorf("AUTH_SESSION_TTL must be positive")
	}

	authEnabled, err := getBool("AUTH_ENABLED", false)
	if err != nil {
		return nil, err
	}

	appBasePath, err := normalizeBasePath(strings.TrimSpace(os.Getenv("APP_BASE_PATH")))
	if err != nil {
		return nil, fmt.Errorf("APP_BASE_PATH is invalid: %w", err)
	}

	cfg := &Config{
		AppPort:                   getString("APP_PORT", "8080"),
		AppBasePath:               appBasePath,
		CPABaseURL:                strings.TrimSpace(os.Getenv("CPA_BASE_URL")),
		CPAManagementKey:          strings.TrimSpace(os.Getenv("CPA_MANAGEMENT_KEY")),
		PollInterval:              pollInterval,
		UsageSyncMode:             usageSyncMode,
		RedisQueueAddr:            strings.TrimSpace(os.Getenv("REDIS_QUEUE_ADDR")),
		RedisQueueKey:             RedisQueueKeyDefault,
		RedisQueueBatchSize:       redisQueueBatchSize,
		RedisQueueIdleInterval:    redisQueueIdleInterval,
		RedisQueueErrorBackoff:    RedisQueueErrorBackoffDefault,
		RedisMetadataSyncInterval: RedisMetadataSyncIntervalDefault,
		SQLitePath:                getString("SQLITE_PATH", "/data/app.db"),
		BackupEnabled:             backupEnabled,
		BackupDir:                 getString("BACKUP_DIR", "/data/backups"),
		BackupInterval:            backupInterval,
		BackupRetentionDays:       backupRetentionDays,
		RequestTimeout:            requestTimeout,
		LogLevel:                  getString("LOG_LEVEL", "info"),
		LogFileEnabled:            logFileEnabled,
		LogDir:                    getString("LOG_DIR", "/data/logs"),
		LogRetentionDays:          logRetentionDays,
		AuthEnabled:               authEnabled,
		LoginPassword:             strings.TrimSpace(os.Getenv("LOGIN_PASSWORD")),
		AuthSessionTTL:            authSessionTTL,
	}
	if cfg.CPABaseURL == "" {
		return nil, fmt.Errorf("CPA_BASE_URL is required")
	}
	if cfg.CPAManagementKey == "" {
		return nil, fmt.Errorf("CPA_MANAGEMENT_KEY is required")
	}
	if cfg.AuthEnabled && cfg.LoginPassword == "" {
		return nil, fmt.Errorf("LOGIN_PASSWORD is required when AUTH_ENABLED is true")
	}

	return cfg, nil
}

func loadDotEnvIfPresent() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	dotEnvPath := filepath.Join(cwd, ".env")
	if _, err := os.Stat(dotEnvPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat .env: %w", err)
	}

	if err := godotenv.Overload(dotEnvPath); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	return nil
}

func normalizeBasePath(value string) (string, error) {
	if value == "" || value == "/" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("must start with '/'")
	}

	normalized := path.Clean(value)
	if normalized == "." || normalized == "/" {
		return "", nil
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized, nil
}

func getString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	return duration, nil
}

func getBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a valid bool: %w", key, err)
	}
	return parsed, nil
}

func getInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}
	return parsed, nil
}
