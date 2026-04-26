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

type Config struct {
	AppPort             string
	AppBasePath         string
	CPABaseURL          string
	CPAManagementKey    string
	PollInterval        time.Duration
	SQLitePath          string
	BackupEnabled       bool
	BackupDir           string
	BackupInterval      time.Duration
	BackupRetentionDays int
	RequestTimeout      time.Duration
	LogLevel            string
	AuthEnabled         bool
	LoginPassword       string
	AuthSessionTTL      time.Duration
}

func LoadFromEnv() (*Config, error) {
	if err := loadDotEnvIfPresent(); err != nil {
		return nil, err
	}

	pollInterval, err := getDuration("POLL_INTERVAL", 5*time.Minute)
	if err != nil {
		return nil, err
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
		AppPort:             getString("APP_PORT", "8080"),
		AppBasePath:         appBasePath,
		CPABaseURL:          strings.TrimSpace(os.Getenv("CPA_BASE_URL")),
		CPAManagementKey:    strings.TrimSpace(os.Getenv("CPA_MANAGEMENT_KEY")),
		PollInterval:        pollInterval,
		SQLitePath:          getString("SQLITE_PATH", "/data/app.db"),
		BackupEnabled:       backupEnabled,
		BackupDir:           getString("BACKUP_DIR", "/data/backups"),
		BackupInterval:      backupInterval,
		BackupRetentionDays: backupRetentionDays,
		RequestTimeout:      requestTimeout,
		LogLevel:            getString("LOG_LEVEL", "info"),
		AuthEnabled:         authEnabled,
		LoginPassword:       strings.TrimSpace(os.Getenv("LOGIN_PASSWORD")),
		AuthSessionTTL:      authSessionTTL,
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
