package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func ListUsedModels(db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var modelsList []string
	if err := db.Model(&entities.UsageEvent{}).
		Distinct().
		Where("trim(model) <> ''").
		Order("model asc").
		Pluck("model", &modelsList).Error; err != nil {
		return nil, fmt.Errorf("list used models: %w", err)
	}

	cleaned := make([]string, 0, len(modelsList))
	seen := make(map[string]struct{}, len(modelsList))
	for _, model := range modelsList {
		trimmed := strings.TrimSpace(model)
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
	if err := db.Select("ID", "Model", "PromptPricePer1M", "CompletionPricePer1M", "CachePricePer1M", "PricePerRequest", "CreatedAt", "UpdatedAt").Order("model asc").Find(&settings).Error; err != nil {
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

	setting := &entities.ModelPriceSetting{}
	if err := db.Select("ID", "Model", "PromptPricePer1M", "CompletionPricePer1M", "CachePricePer1M", "PricePerRequest", "CreatedAt", "UpdatedAt").Where("model = ?", modelName).First(setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			setting = &entities.ModelPriceSetting{Model: modelName}
		} else {
			return nil, fmt.Errorf("load pricing setting: %w", err)
		}
	}

	setting.Model = modelName
	setting.PromptPricePer1M = input.PromptPricePer1M
	setting.CompletionPricePer1M = input.CompletionPricePer1M
	setting.CachePricePer1M = input.CachePricePer1M
	setting.PricePerRequest = input.PricePerRequest

	if err := db.Save(setting).Error; err != nil {
		return nil, fmt.Errorf("save pricing setting: %w", err)
	}

	return setting, nil
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
