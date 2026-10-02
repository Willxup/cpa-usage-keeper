package test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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

	cfg := databasePoolTestConfig(databasePath)
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	err = application.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pricing snapshot") {
		t.Fatalf("expected pricing snapshot initialization error, got %v", err)
	}

	// 故障期间数据库继续可供启动接收写入；真正关闭由 App.Close 完成。
	if application.DB == nil {
		t.Fatal("initialization failure closed durable inbox database")
	}
	verificationSQL, sqlErr := application.DB.DB()
	if sqlErr != nil || verificationSQL.Ping() != nil {
		t.Fatalf("failed startup database unavailable: %v", sqlErr)
	}
}
