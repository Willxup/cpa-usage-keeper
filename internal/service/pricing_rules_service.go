package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"

	"gorm.io/gorm"
)

var (
	ErrPricingModelNotFound = errors.New("pricing model not found")
	ErrInvalidPricingRule   = errors.New("invalid pricing rule")
)

func (s *pricingService) ListPricingRules(_ context.Context, model string) ([]servicedto.PricingRule, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", ErrInvalidPricingRule)
	}
	config, ok := s.catalog.Snapshot().ModelConfig(model)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPricingModelNotFound, model)
	}
	return servicePricingRules(config.Rules), nil
}

func (s *pricingService) ReplacePricingRules(ctx context.Context, input servicedto.ReplacePricingRulesInput) ([]servicedto.PricingRule, error) {
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", ErrInvalidPricingRule)
	}
	normalized, err := normalizePricingRuleInputs(input.Rules)
	if err != nil {
		return nil, err
	}
	candidate, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		_, replaceErr := repository.ReplaceModelPriceRules(tx, model, normalized)
		return replaceErr
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrPricingModelNotFound, model)
		}
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPricingRule, err)
		}
		return nil, err
	}
	config, ok := candidate.ModelConfig(model)
	if !ok {
		return nil, fmt.Errorf("published pricing model %q is missing", model)
	}
	return servicePricingRules(config.Rules), nil
}

func normalizePricingRuleInputs(inputs []servicedto.PricingRuleInput) ([]repodto.ModelPriceRuleInput, error) {
	normalized := make([]repodto.ModelPriceRuleInput, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for index := range inputs {
		field, err := pricing.ParseRuleField(inputs[index].Key)
		if err != nil {
			return nil, fmt.Errorf("%w: rule %d: %v", ErrInvalidPricingRule, index, err)
		}
		value := strings.TrimSpace(inputs[index].Value)
		if value == "" {
			return nil, fmt.Errorf("%w: rule %d value is required", ErrInvalidPricingRule, index)
		}
		multiplier := 1.0
		if inputs[index].Multiplier != nil {
			multiplier = *inputs[index].Multiplier
		}
		if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return nil, fmt.Errorf("%w: rule %d multiplier must be a finite non-negative number", ErrInvalidPricingRule, index)
		}
		identity := field.String() + "\x00" + value
		if _, exists := seen[identity]; exists {
			return nil, fmt.Errorf("%w: duplicate rule %s=%q", ErrInvalidPricingRule, field, value)
		}
		seen[identity] = struct{}{}
		normalized[index] = repodto.ModelPriceRuleInput{Key: field.String(), Value: value, Multiplier: multiplier}
	}
	return normalized, nil
}

func servicePricingRules(rules []pricing.RuleConfig) []servicedto.PricingRule {
	result := make([]servicedto.PricingRule, len(rules))
	for index := range rules {
		result[index] = servicedto.PricingRule{
			Key:        rules[index].Key,
			Value:      rules[index].Value,
			Multiplier: rules[index].Multiplier,
		}
	}
	return result
}
