package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func TestListUsedModelsReturnsDistinctSortedModels(t *testing.T) {
	db := openPricingTestDatabase(t)
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{
		Name:         "provider-a",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "auth-a",
		Type:         "claude",
		Provider:     "provider-a",
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}

	events := []entities.UsageEvent{
		{EventKey: "1", Model: "claude-sonnet", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Unix(1, 0)},
		{EventKey: "2", Model: "claude-haiku", Timestamp: time.Unix(2, 0)},
		{EventKey: "3", Model: "claude-sonnet", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Unix(3, 0)},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}
	if err := AggregateUsageModels(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate usage models: %v", err)
	}

	modelsList, err := ListUsedModels(db)
	if err != nil {
		t.Fatalf("list used models: %v", err)
	}
	expected := []string{"claude-haiku", "claude-sonnet", "provider-a/claude-sonnet"}
	if strings.Join(modelsList, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected models: %#v", modelsList)
	}

	options, err := ListUsedModelOptions(db)
	if err != nil {
		t.Fatalf("list used model options: %v", err)
	}
	optionValues := make([]string, 0, len(options))
	for _, option := range options {
		optionValues = append(optionValues, option.Value+"|"+option.Source+"|"+option.Model)
	}
	expectedOptions := []string{
		"claude-haiku||claude-haiku",
		"claude-sonnet||claude-sonnet",
		"provider-a/claude-sonnet|provider-a|claude-sonnet",
	}
	if strings.Join(optionValues, ",") != strings.Join(expectedOptions, ",") {
		t.Fatalf("unexpected model options: %#v", options)
	}
}

func TestUpsertModelPriceSettingCreatesAndUpdatesRow(t *testing.T) {
	db := openPricingTestDatabase(t)

	created, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
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

	updated, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
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
