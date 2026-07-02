package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func TestListUsedModelsReturnsDistinctSortedModels(t *testing.T) {
	db := openTestDatabase(t)

	events := []entities.UsageEvent{
		{EventKey: "1", Model: "claude-sonnet", Timestamp: time.Unix(1, 0)},
		{EventKey: "2", Model: "claude-haiku", Timestamp: time.Unix(2, 0)},
		{EventKey: "3", Model: "claude-sonnet", Timestamp: time.Unix(3, 0)},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}

	modelsList, err := ListUsedModels(db)
	if err != nil {
		t.Fatalf("list used models: %v", err)
	}
	if len(modelsList) != 2 || modelsList[0] != "claude-haiku" || modelsList[1] != "claude-sonnet" {
		t.Fatalf("unexpected models: %#v", modelsList)
	}
}

func TestUpsertModelPriceSettingCreatesAndUpdatesRow(t *testing.T) {
	db := openTestDatabase(t)

	created, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("create pricing setting: %v", err)
	}
	if created.Model != "claude-sonnet" || created.PricingStyle != "claude" || created.PromptPricePer1M != 3 || created.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected created setting: %#v", created)
	}

	updated, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PricingStyle:         "",
		PromptPricePer1M:     4,
		CompletionPricePer1M: 16,
		CachePricePer1M:      0.4,
	})
	if err != nil {
		t.Fatalf("update pricing setting: %v", err)
	}
	if updated.ID != created.ID || updated.PricingStyle != "openai" || updated.PromptPricePer1M != 4 || updated.CachePricePer1M != 0.4 || updated.CacheCreationPricePer1M != 0 {
		t.Fatalf("unexpected updated setting: %#v", updated)
	}

	settings, err := ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("list pricing settings: %v", err)
	}
	if len(settings) != 1 || settings[0].CompletionPricePer1M != 16 || settings[0].PricingStyle != "openai" {
		t.Fatalf("unexpected settings: %#v", settings)
	}
}

func TestUpsertModelPriceSettingRejectsUnknownPricingStyle(t *testing.T) {
	db := openTestDatabase(t)

	_, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:        "claude-sonnet",
		PricingStyle: "legacy",
	})
	if err == nil || !strings.Contains(err.Error(), "pricing_style") {
		t.Fatalf("expected pricing_style validation error, got %v", err)
	}
}

func TestDeleteModelPriceSettingDeletesOnlyTheTargetModel(t *testing.T) {
	db := openTestDatabase(t)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	}); err != nil {
		t.Fatalf("seed target pricing setting: %v", err)
	}
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "openai/gpt-4.1",
		PromptPricePer1M:     2,
		CompletionPricePer1M: 8,
		CachePricePer1M:      0.2,
	}); err != nil {
		t.Fatalf("seed preserved pricing setting: %v", err)
	}

	if err := DeleteModelPriceSetting(db, " claude-sonnet "); err != nil {
		t.Fatalf("DeleteModelPriceSetting returned error: %v", err)
	}
	settings, err := ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("ListModelPriceSettings returned error: %v", err)
	}
	if len(settings) != 1 || settings[0].Model != "openai/gpt-4.1" {
		t.Fatalf("expected only openai/gpt-4.1 pricing to remain, got %#v", settings)
	}
	if err := DeleteModelPriceSetting(db, "openai/gpt-4.1"); err != nil {
		t.Fatalf("DeleteModelPriceSetting returned error for slash model: %v", err)
	}
	settings, err = ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("ListModelPriceSettings returned error after slash delete: %v", err)
	}
	if len(settings) != 0 {
		t.Fatalf("expected slash model pricing to be deleted, got %#v", settings)
	}

	if err := DeleteModelPriceSetting(db, " "); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("expected empty model validation error, got %v", err)
	}
}

func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}

func closeTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
}
