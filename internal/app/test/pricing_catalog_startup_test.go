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
	cfg := databasePoolTestConfig(databasePath)
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
	for _, prefix := range []string{"cpa-usage-keeper-error-", "cpa-usage-keeper-"} {
		logPath := filepath.Join(logDir, prefix+time.Now().Format("2006-01-02")+".log")
		contents, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read startup log %s: %v", logPath, err)
		}
		if !strings.Contains(string(contents), "| fatal | initialize app") || !strings.Contains(string(contents), "pricing snapshot") {
			t.Fatalf("expected initialization failure before closing %s, got %q", logPath, contents)
		}
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
	if sqlErr != nil {
		t.Fatalf("get verification SQL database: %v", sqlErr)
	}
	if err := verificationSQL.Close(); err != nil {
		t.Fatalf("close verification database: %v", err)
	}
}
