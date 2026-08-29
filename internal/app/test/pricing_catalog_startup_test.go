package test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestPricingCatalogStartupFailsWhenPersistedSnapshotIsInvalid(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "invalid-pricing.db")
	seedDB, err := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	setting, err := repository.UpsertModelPriceSetting(seedDB, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 1})
	if err != nil {
		t.Fatalf("seed model price: %v", err)
	}
	if err := seedDB.Create(&entities.ModelPriceRule{
		ModelPriceSettingID: setting.ID,
		Key:                 "provider",
		Value:               "openai",
		Multiplier:          2,
	}).Error; err != nil {
		t.Fatalf("seed invalid model price rule: %v", err)
	}
	seedSQL, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed SQL DB: %v", err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatalf("close seed database: %v", err)
	}

	logDir := t.TempDir()
	cfg := pricingCatalogStartupConfig(databasePath)
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	previousStderr := os.Stderr
	stderrReader, stderrWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("create stderr pipe: %v", pipeErr)
	}
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	})
	application, err := keeperapp.NewWithConfig(cfg)
	if closeErr := stderrWriter.Close(); closeErr != nil {
		t.Fatalf("close stderr writer: %v", closeErr)
	}
	os.Stderr = previousStderr
	console, consoleErr := io.ReadAll(stderrReader)
	if consoleErr != nil {
		t.Fatalf("read startup stderr: %v", consoleErr)
	}
	if application != nil {
		_ = application.Close()
		t.Fatal("expected invalid pricing snapshot to prevent App construction")
	}
	if err == nil || !strings.Contains(err.Error(), "pricing snapshot") {
		t.Fatalf("expected pricing snapshot startup error, got %v", err)
	}
	if !keeperapp.IsInitializationErrorLogged(err) {
		t.Fatalf("expected initialization error to record that it was already logged, got %T", err)
	}
	errorLogPath := filepath.Join(logDir, "cpa-usage-keeper-error-"+time.Now().Format("2006-01-02")+".log")
	errorLog, readErr := os.ReadFile(errorLogPath)
	if readErr != nil {
		t.Fatalf("read startup error log: %v", readErr)
	}
	if !strings.Contains(string(errorLog), "| fatal | initialize app") || !strings.Contains(string(errorLog), "pricing snapshot") {
		t.Fatalf("expected initialization failure before log close, got %q", errorLog)
	}
	combinedLogPath := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	combinedLog, readErr := os.ReadFile(combinedLogPath)
	if readErr != nil {
		t.Fatalf("read startup combined log: %v", readErr)
	}
	if !strings.Contains(string(combinedLog), "| fatal | initialize app") || !strings.Contains(string(combinedLog), "pricing snapshot") {
		t.Fatalf("expected initialization failure in combined log, got %q", combinedLog)
	}
	if count := strings.Count(string(console), "initialize app"); count != 1 {
		t.Fatalf("expected one initialization failure on stderr, got %d in %q", count, console)
	}

	// 构造失败必须释放 reader/writer，随后应能立即重新打开同一个数据库。
	verificationDB, openErr := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if openErr != nil {
		t.Fatalf("expected failed App construction to release database pools: %v", openErr)
	}
	verificationSQL, sqlErr := verificationDB.DB()
	if sqlErr == nil {
		_ = verificationSQL.Close()
	}
}

func pricingCatalogStartupConfig(databasePath string) config.Config {
	return config.Config{
		AppPort:                "invalid-port",
		CPABaseURL:             "https://cpa.example.com",
		CPAManagementKey:       "secret",
		RedisQueueIdleInterval: time.Second,
		MetadataSyncInterval:   30 * time.Second,
		SQLitePath:             databasePath,
		RequestTimeout:         5 * time.Second,
		LogLevel:               "info",
		LogFileEnabled:         false,
		LogRetentionDays:       7,
	}
}
