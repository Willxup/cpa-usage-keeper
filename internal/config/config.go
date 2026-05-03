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

	"cpa-usage-keeper/internal/cpa"
	"github.com/joho/godotenv"
)

const (
	DefaultTimeZone                  = "Asia/Shanghai"
	RedisQueueKeyDefault             = cpa.ManagementUsageQueueKey
	RedisQueueErrorBackoffDefault    = 10 * time.Second
	RedisMetadataSyncIntervalDefault = 30 * time.Second
)

var (
	DefaultWorkDir      = filepath.Join(".", "data")
	DefaultSQLitePath   = filepath.Join(DefaultWorkDir, "app.db")
	DefaultLogDir       = filepath.Join(DefaultWorkDir, "logs")
	DefaultBackupDir    = filepath.Join(DefaultWorkDir, "backups")
	workDirDatabaseName = filepath.Base(DefaultSQLitePath)
	workDirLogsName     = filepath.Base(DefaultLogDir)
	workDirBackupsName  = filepath.Base(DefaultBackupDir)
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
	// PollInterval 是 legacy export 拉取间隔。
	PollInterval time.Duration
	// UsageSyncMode 决定使用 auto、redis 或 legacy_export；auto 会在启动时解析为一种有效模式。
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
	// WorkDir 是应用工作目录，数据库、日志和备份默认从这里派生。
	WorkDir string
	// SQLitePath 是 SQLite 数据库文件路径。
	SQLitePath string
	// BackupEnabled 控制是否保存 SQLite 数据库备份文件。
	BackupEnabled bool
	// BackupDir 是 SQLite 数据库备份目录。
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

type LoadOptions struct {
	EnvFile string
}

var executableDir = func() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(executablePath), nil
}

func LoadFromEnv() (*Config, error) {
	return Load(LoadOptions{})
}

func Load(options LoadOptions) (*Config, error) {
	envBaseDir, err := loadDotEnv(options)
	if err != nil {
		return nil, err
	}
	if err := applyProjectTimeZone(); err != nil {
		return nil, err
	}

	usageSyncMode := getString("USAGE_SYNC_MODE", "auto")
	if usageSyncMode != "auto" && usageSyncMode != "redis" && usageSyncMode != "legacy_export" {
		return nil, fmt.Errorf("USAGE_SYNC_MODE must be one of auto, redis, legacy_export")
	}

	pollInterval, err := getDuration("POLL_INTERVAL", 5*time.Minute)
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

	backupInterval, err := getDuration("BACKUP_INTERVAL", 24*time.Hour)
	if err != nil {
		return nil, err
	}
	if backupInterval <= 0 {
		return nil, fmt.Errorf("BACKUP_INTERVAL must be positive")
	}

	backupRetentionDays, err := getInt("BACKUP_RETENTION_DAYS", 7)
	if err != nil {
		return nil, err
	}
	if backupRetentionDays < 0 {
		return nil, fmt.Errorf("BACKUP_RETENTION_DAYS must be non-negative")
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

	workDir := getString("WORK_DIR", DefaultWorkDir)

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
		WorkDir:                   workDir,
		SQLitePath:                filepath.Join(workDir, workDirDatabaseName),
		BackupEnabled:             backupEnabled,
		BackupDir:                 filepath.Join(workDir, workDirBackupsName),
		BackupInterval:            backupInterval,
		BackupRetentionDays:       backupRetentionDays,
		RequestTimeout:            requestTimeout,
		LogLevel:                  getString("LOG_LEVEL", "info"),
		LogFileEnabled:            logFileEnabled,
		LogDir:                    filepath.Join(workDir, workDirLogsName),
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
	cfg.resolveRelativePaths(envBaseDir)

	return cfg, nil
}

func applyProjectTimeZone() error {
	zoneName := strings.TrimSpace(os.Getenv("TZ"))
	if zoneName == "" {
		zoneName = DefaultTimeZone
		if err := os.Setenv("TZ", zoneName); err != nil {
			return fmt.Errorf("set default TZ: %w", err)
		}
	}
	location, err := time.LoadLocation(zoneName)
	if err != nil {
		return fmt.Errorf("TZ is invalid: %w", err)
	}
	time.Local = location
	return nil
}

func loadDotEnv(options LoadOptions) (string, error) {
	if strings.TrimSpace(options.EnvFile) != "" {
		return loadDotEnvFile(options.EnvFile, true)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	if loaded, err := loadOptionalDotEnv(filepath.Join(cwd, ".env")); err != nil || loaded {
		if loaded {
			return cwd, err
		}
		return "", err
	}

	exeDir, err := executableDir()
	if err != nil {
		return "", fmt.Errorf("get executable directory: %w", err)
	}
	loaded, err := loadOptionalDotEnv(filepath.Join(exeDir, ".env"))
	if loaded {
		return exeDir, err
	}
	return "", err
}

func loadOptionalDotEnv(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat .env: %w", err)
	}
	if err := godotenv.Overload(path); err != nil {
		return false, fmt.Errorf("load .env: %w", err)
	}
	return true, nil
}

func loadDotEnvFile(path string, required bool) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return "", nil
		}
		return "", fmt.Errorf("stat env file: %w", err)
	}
	if err := godotenv.Overload(path); err != nil {
		return "", fmt.Errorf("load env file: %w", err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve env file path: %w", err)
	}
	return filepath.Dir(absolutePath), nil
}

func (cfg *Config) resolveRelativePaths(baseDir string) {
	if baseDir == "" {
		return
	}
	cfg.WorkDir = resolveRelativePath(baseDir, cfg.WorkDir)
	cfg.SQLitePath = resolveRelativePath(baseDir, cfg.SQLitePath)
	cfg.LogDir = resolveRelativePath(baseDir, cfg.LogDir)
	cfg.BackupDir = resolveRelativePath(baseDir, cfg.BackupDir)
}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
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
