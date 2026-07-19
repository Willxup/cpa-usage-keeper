package repository

import (
	"context"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"gorm.io/gorm"
)

type UsageCostSubject struct {
	Model        string
	ModelAlias   string
	ServiceTier  string
	ExecutorType string
	Tokens       helper.UsageTokenCostInput
}

type UsageCostResult struct {
	Cost         helper.UsageTokenCostBreakdown
	Available    bool
	PricingStyle string
	MatchedModel string
	MatchedBy    string
}

type UsageCostResolver struct {
	pricesByModel map[string]entities.ModelPriceSetting
}

func NewUsageCostResolver(ctx context.Context, db *gorm.DB) (*UsageCostResolver, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	settings, err := ListModelPriceSettings(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	resolver := &UsageCostResolver{pricesByModel: make(map[string]entities.ModelPriceSetting, len(settings))}
	for _, setting := range settings {
		model := strings.TrimSpace(setting.Model)
		if model == "" {
			continue
		}
		resolver.pricesByModel[model] = setting
	}
	return resolver, nil
}

func (r *UsageCostResolver) Calculate(subject UsageCostSubject) UsageCostResult {
	pricing, matchedModel, matchedBy, ok := r.matchPricing(subject.Model, subject.ModelAlias)
	if !ok {
		return UsageCostResult{Available: !helper.UsageTokenInputRequiresPricing(subject.Tokens)}
	}
	return UsageCostResult{
		Cost:         helper.CalculateUsageTokenCostBreakdown(subject.Tokens, pricing),
		Available:    true,
		PricingStyle: pricing.PricingStyle,
		MatchedModel: matchedModel,
		MatchedBy:    matchedBy,
	}
}

func (r *UsageCostResolver) CalculateEvent(event entities.UsageEvent) UsageCostResult {
	modelAlias := ""
	if event.ModelAlias != nil {
		modelAlias = *event.ModelAlias
	}
	return r.Calculate(UsageCostSubject{
		Model:        event.Model,
		ModelAlias:   modelAlias,
		ServiceTier:  event.ServiceTier,
		ExecutorType: event.ExecutorType,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         event.InputTokens,
			OutputTokens:        event.OutputTokens,
			CacheReadTokens:     event.CacheReadTokens,
			CacheCreationTokens: event.CacheCreationTokens,
		},
	})
}

func (r *UsageCostResolver) matchPricing(model string, modelAlias string) (entities.ModelPriceSetting, string, string, bool) {
	if r == nil {
		return entities.ModelPriceSetting{}, "", "", false
	}
	modelName := strings.TrimSpace(model)
	alias := strings.TrimSpace(modelAlias)
	if modelName != "" {
		if pricing, ok := r.pricesByModel[modelName]; ok {
			return pricing, modelName, "model", true
		}
	}
	if alias != "" {
		if pricing, ok := r.pricesByModel[alias]; ok {
			return pricing, alias, "model_alias", true
		}
	}
	// CPA may append a bracketed routing variant to the reported model or alias,
	// for example "kimi-k3[1m]". Keep exact prices authoritative, then inherit
	// the configured base-model price when only the routing variant differs.
	if baseModel := pricingBaseModel(modelName); baseModel != "" {
		if pricing, ok := r.pricesByModel[baseModel]; ok {
			return pricing, baseModel, "model_base", true
		}
	}
	if baseAlias := pricingBaseModel(alias); baseAlias != "" {
		if pricing, ok := r.pricesByModel[baseAlias]; ok {
			return pricing, baseAlias, "model_alias_base", true
		}
	}
	return entities.ModelPriceSetting{}, "", "", false
}

func pricingBaseModel(model string) string {
	if model == "" || !strings.HasSuffix(model, "]") {
		return ""
	}
	open := strings.LastIndexByte(model, '[')
	if open <= 0 || open == len(model)-2 {
		return ""
	}
	return strings.TrimSpace(model[:open])
}

func newUsageCostResolverForDB(db *gorm.DB) (*UsageCostResolver, error) {
	return NewUsageCostResolver(gormDBContext(db), db)
}

func gormDBContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}
