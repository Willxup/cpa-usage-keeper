package repository

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

var modelPriceSettingColumns = []string{
	"id",
	"model",
	"pricing_style",
	"prompt_price_per1_m",
	"completion_price_per1_m",
	"cache_price_per1_m",
	"cache_creation_price_per1_m",
	"price_multiplier",
	"created_at",
	"updated_at",
}

func ListUsedModels(db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var modelsList []sql.NullString
	if err := db.Model(&entities.UsageEvent{}).
		Distinct().
		Pluck("model", &modelsList).Error; err != nil {
		return nil, fmt.Errorf("list used models: %w", err)
	}

	cleaned := make([]string, 0, len(modelsList))
	seen := make(map[string]struct{}, len(modelsList))
	for _, model := range modelsList {
		if !model.Valid {
			continue
		}
		trimmed := strings.TrimSpace(model.String)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	sort.Strings(cleaned)
	return cleaned, nil
}

func ListModelPriceSettings(db *gorm.DB) ([]entities.ModelPriceSetting, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var settings []entities.ModelPriceSetting
	if err := db.Select(modelPriceSettingColumns).Order("model asc").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("list pricing settings: %w", err)
	}
	return settings, nil
}

func UpsertModelPriceSetting(db *gorm.DB, input dto.ModelPriceSettingInput) (*entities.ModelPriceSetting, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	pricingStyle, err := normalizeModelPricingStyle(input.PricingStyle)
	if err != nil {
		return nil, err
	}

	setting := &entities.ModelPriceSetting{}
	if err := db.Select(modelPriceSettingColumns).Where("model = ?", modelName).First(setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			setting = &entities.ModelPriceSetting{Model: modelName}
		} else {
			return nil, fmt.Errorf("load pricing setting: %w", err)
		}
	}

	setting.Model = modelName
	setting.PricingStyle = pricingStyle
	setting.PromptPricePer1M = input.PromptPricePer1M
	setting.CompletionPricePer1M = input.CompletionPricePer1M
	setting.CachePricePer1M = input.CachePricePer1M
	setting.CacheCreationPricePer1M = input.CacheCreationPricePer1M
	multiplier, err := modelPriceMultiplierInputValue(input.PriceMultiplier)
	if err != nil {
		return nil, err
	}
	setting.PriceMultiplier = &multiplier

	if err := db.Save(setting).Error; err != nil {
		return nil, fmt.Errorf("save pricing setting: %w", err)
	}

	return setting, nil
}

func modelPriceMultiplierInputValue(input *float64) (float64, error) {
	if input == nil {
		return 1, nil
	}
	multiplier := *input
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 0, fmt.Errorf("price_multiplier must be non-negative")
	}
	return multiplier, nil
}

func normalizeModelPricingStyle(style string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "":
		return entities.ModelPricingStyleOpenAI, nil
	case entities.ModelPricingStyleOpenAI:
		return entities.ModelPricingStyleOpenAI, nil
	case entities.ModelPricingStyleClaude:
		return entities.ModelPricingStyleClaude, nil
	default:
		return "", fmt.Errorf("pricing_style must be openai or claude")
	}
}

func DeleteModelPriceSetting(db *gorm.DB, model string) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	modelName := strings.TrimSpace(model)
	if modelName == "" {
		return fmt.Errorf("model is required")
	}
	if err := db.Where("model = ?", modelName).Delete(&entities.ModelPriceSetting{}).Error; err != nil {
		return fmt.Errorf("delete pricing setting: %w", err)
	}
	return nil
}
