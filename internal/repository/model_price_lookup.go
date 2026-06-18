package repository

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
)

const (
	modelPriceServiceTierDefault  = "default"
	modelPriceServiceTierPriority = "priority"
)

type modelPriceLookup struct {
	byModel map[string]map[string]entities.ModelPriceSetting
}

func newModelPriceLookup(settings []entities.ModelPriceSetting) modelPriceLookup {
	lookup := modelPriceLookup{byModel: make(map[string]map[string]entities.ModelPriceSetting, len(settings))}
	for _, setting := range settings {
		model := strings.TrimSpace(setting.Model)
		if model == "" {
			continue
		}
		serviceTier := canonicalPricingServiceTier(setting.ServiceTier)
		perTier := lookup.byModel[model]
		if perTier == nil {
			perTier = make(map[string]entities.ModelPriceSetting, 2)
			lookup.byModel[model] = perTier
		}
		setting.Model = model
		setting.ServiceTier = serviceTier
		perTier[serviceTier] = setting
	}
	return lookup
}

func (l modelPriceLookup) Find(model string, serviceTier string) (entities.ModelPriceSetting, bool) {
	perTier := l.byModel[strings.TrimSpace(model)]
	if len(perTier) == 0 {
		return entities.ModelPriceSetting{}, false
	}
	for _, candidateTier := range pricingLookupServiceTierCandidates(serviceTier) {
		if setting, ok := perTier[candidateTier]; ok {
			return setting, true
		}
	}
	return entities.ModelPriceSetting{}, false
}

func canonicalPricingServiceTier(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "standard" {
		return modelPriceServiceTierDefault
	}
	return normalized
}

func normalizeModelPriceSettingServiceTierInput(value string) (string, error) {
	normalized := canonicalPricingServiceTier(value)
	switch normalized {
	case "", modelPriceServiceTierDefault, modelPriceServiceTierPriority:
		return normalized, nil
	default:
		return "", fmt.Errorf("service_tier must be empty, default, or priority")
	}
}

func pricingLookupServiceTierCandidates(value string) []string {
	switch canonicalPricingServiceTier(value) {
	case "", modelPriceServiceTierDefault:
		return []string{modelPriceServiceTierDefault, ""}
	case modelPriceServiceTierPriority:
		return []string{modelPriceServiceTierPriority, ""}
	default:
		return []string{canonicalPricingServiceTier(value), ""}
	}
}

func usageOverviewServiceTier(value string) string {
	switch canonicalPricingServiceTier(value) {
	case "", modelPriceServiceTierDefault:
		return modelPriceServiceTierDefault
	default:
		return canonicalPricingServiceTier(value)
	}
}
