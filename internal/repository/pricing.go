package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"gorm.io/gorm"
)

type usageModelPricingRow struct {
	Model     string
	AuthType  string
	AuthIndex string
}

type usagePricingSources struct {
	byTypedAuthIndex map[string]string
	byAuthIndex      map[string]string
}

type usagePricingContext struct {
	settingsByKey map[string]entities.ModelPriceSetting
	sources       usagePricingSources
}

func ListUsedModels(db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var rows []usageModelPricingRow
	if err := db.Model(&entities.UsageModel{}).
		Select("model, auth_type, auth_index").
		Where("trim(model) <> ''").
		Order("model asc, auth_type asc, auth_index asc").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list used models: %w", err)
	}
	sources, err := loadUsagePricingSources(db)
	if err != nil {
		return nil, err
	}

	models := make([]string, 0, len(rows)*2)
	seen := make(map[string]struct{}, len(rows)*2)
	appendModel := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	for _, row := range rows {
		model := strings.TrimSpace(row.Model)
		if model == "" {
			continue
		}
		appendModel(model)
		if source := sources.sourceFor(row.AuthType, row.AuthIndex); source != "" {
			appendModel(usagePricingKey(source, model))
		}
	}
	sort.Strings(models)
	return models, nil
}

func ListModelPriceSettings(db *gorm.DB) ([]entities.ModelPriceSetting, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var settings []entities.ModelPriceSetting
	if err := db.Select("ID", "Model", "PromptPricePer1M", "CompletionPricePer1M", "CachePricePer1M", "CreatedAt", "UpdatedAt").Order("model asc").Find(&settings).Error; err != nil {
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
	if err := db.Select("ID", "Model", "PromptPricePer1M", "CompletionPricePer1M", "CachePricePer1M", "CreatedAt", "UpdatedAt").Where("model = ?", modelName).First(setting).Error; err != nil {
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

func loadUsagePricingContext(db *gorm.DB) (usagePricingContext, error) {
	settings, err := loadPriceSettingsByModel(db)
	if err != nil {
		return usagePricingContext{}, err
	}
	sources, err := loadUsagePricingSources(db)
	if err != nil {
		return usagePricingContext{}, err
	}
	return usagePricingContext{settingsByKey: settings, sources: sources}, nil
}

func (c usagePricingContext) lookup(model, authType, authIndex string) (entities.ModelPriceSetting, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return entities.ModelPriceSetting{}, false
	}
	if source := c.sources.sourceFor(authType, authIndex); source != "" {
		if pricing, ok := c.settingsByKey[usagePricingKey(source, model)]; ok {
			return pricing, true
		}
	}
	pricing, ok := c.settingsByKey[model]
	return pricing, ok
}

func loadUsagePricingSources(db *gorm.DB) (usagePricingSources, error) {
	if db == nil {
		return usagePricingSources{}, fmt.Errorf("database is nil")
	}
	var identities []entities.UsageIdentity
	if err := db.Select("auth_type, auth_type_name, identity, name, type, provider, prefix, base_url").
		Find(&identities).Error; err != nil {
		return usagePricingSources{}, fmt.Errorf("load usage pricing sources: %w", err)
	}
	return buildUsagePricingSources(identities), nil
}

func buildUsagePricingSources(identities []entities.UsageIdentity) usagePricingSources {
	sources := usagePricingSources{
		byTypedAuthIndex: map[string]string{},
		byAuthIndex:      map[string]string{},
	}
	ambiguous := map[string]struct{}{}
	for _, identity := range identities {
		if identity.AuthType != entities.UsageIdentityAuthTypeAIProvider {
			continue
		}
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" {
			continue
		}
		source := strings.TrimSpace(helper.UsageIdentityDisplayName(identity))
		if source == "" {
			continue
		}
		sources.byTypedAuthIndex[usagePricingSourceTypedKey(usageIdentityEventAuthType(identity), authIndex)] = source
		if existing, ok := sources.byAuthIndex[authIndex]; ok && existing != source {
			ambiguous[authIndex] = struct{}{}
			continue
		}
		sources.byAuthIndex[authIndex] = source
	}
	for authIndex := range ambiguous {
		delete(sources.byAuthIndex, authIndex)
	}
	return sources
}

func (s usagePricingSources) sourceFor(authType, authIndex string) string {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return ""
	}
	if source := s.byTypedAuthIndex[usagePricingSourceTypedKey(authType, authIndex)]; source != "" {
		return source
	}
	return s.byAuthIndex[authIndex]
}

func usageIdentityEventAuthType(identity entities.UsageIdentity) string {
	switch identity.AuthType {
	case entities.UsageIdentityAuthTypeAuthFile:
		return "oauth"
	case entities.UsageIdentityAuthTypeAIProvider:
		return "apikey"
	default:
		return strings.TrimSpace(identity.AuthTypeName)
	}
}

func usagePricingSourceTypedKey(authType, authIndex string) string {
	return strings.TrimSpace(authType) + "\x00" + strings.TrimSpace(authIndex)
}

func usagePricingKey(source, model string) string {
	source = strings.TrimSpace(source)
	model = strings.TrimSpace(model)
	if source == "" {
		return model
	}
	return source + "/" + model
}

func loadPriceSettingsByModel(db *gorm.DB) (map[string]entities.ModelPriceSetting, error) {
	settings, err := ListModelPriceSettings(db)
	if err != nil {
		return nil, err
	}
	result := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		model := strings.TrimSpace(setting.Model)
		if model == "" {
			continue
		}
		result[model] = setting
	}
	return result, nil
}

func pricingContextFromSettings(settings map[string]entities.ModelPriceSetting) usagePricingContext {
	return usagePricingContext{settingsByKey: settings}
}

func pricingContextWithSources(settings map[string]entities.ModelPriceSetting, identities []entities.UsageIdentity) usagePricingContext {
	return usagePricingContext{settingsByKey: settings, sources: buildUsagePricingSources(identities)}
}
