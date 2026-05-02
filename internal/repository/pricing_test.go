package repository

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func TestListUsedModelsReturnsDistinctSortedModels(t *testing.T) {
	db := openPricingTestDatabase(t)

	events := []models.UsageEvent{
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
	db := openPricingTestDatabase(t)

	created, err := UpsertModelPriceSetting(db, ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("create pricing setting: %v", err)
	}
	if created.Model != "claude-sonnet" || created.PromptPricePer1M != 3 {
		t.Fatalf("unexpected created setting: %#v", created)
	}

	updated, err := UpsertModelPriceSetting(db, ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     4,
		CompletionPricePer1M: 16,
		CachePricePer1M:      0.4,
	})
	if err != nil {
		t.Fatalf("update pricing setting: %v", err)
	}
	if updated.ID != created.ID || updated.PromptPricePer1M != 4 || updated.CachePricePer1M != 0.4 {
		t.Fatalf("unexpected updated setting: %#v", updated)
	}

	settings, err := ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("list pricing settings: %v", err)
	}
	if len(settings) != 1 || settings[0].CompletionPricePer1M != 16 {
		t.Fatalf("unexpected settings: %#v", settings)
	}
}

func openPricingTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "pricing.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
