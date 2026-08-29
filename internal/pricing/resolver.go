package pricing

import (
	"cpa-usage-keeper/internal/helper"
)

// CostSubject 是所有 usage 来源进入计价领域的唯一固定输入。
type CostSubject struct {
	Dimensions UsageDimensions
	Tokens     helper.UsageTokenCostInput
}

func NewCostSubject(dimensions UsageDimensions, tokens helper.UsageTokenCostInput) CostSubject {
	return CostSubject{
		Dimensions: canonicalizeUsageDimensions(dimensions),
		Tokens:     tokens,
	}
}

type CostResult struct {
	Cost           helper.UsageTokenCostBreakdown
	Available      bool
	PricingStyle   string
	MatchedModel   string
	MatchedBy      string
	RuleMultiplier float64
}

// Resolver 在创建时固定绑定一个 Snapshot，确保单个响应不会混用新旧价格。
type Resolver struct {
	snapshot *Snapshot
}

func (r Resolver) ActiveFields() ActiveFields {
	if r.snapshot == nil {
		return 0
	}
	return r.snapshot.activeFields
}

func (r Resolver) Calculate(subject CostSubject) CostResult {
	model, matchedModel, matchedBy, found := r.matchModel(subject.Dimensions)
	if !found {
		return CostResult{
			Available:      !helper.UsageTokenInputRequiresPricing(subject.Tokens),
			RuleMultiplier: 1,
		}
	}

	breakdown := helper.CalculateUsageTokenCostBreakdown(subject.Tokens, model.pricing)
	ruleMultiplier := 1.0
	if model.pricing.PriceMultiplier == nil || *model.pricing.PriceMultiplier != 0 {
		ruleMultiplier = matchingRuleMultiplier(model.rules, subject.Dimensions)
		breakdown = helper.ScaleUsageTokenCostBreakdown(breakdown, ruleMultiplier)
	}
	return CostResult{
		Cost:           breakdown,
		Available:      true,
		PricingStyle:   model.pricing.PricingStyle,
		MatchedModel:   matchedModel,
		MatchedBy:      matchedBy,
		RuleMultiplier: ruleMultiplier,
	}
}

func (r Resolver) matchModel(dimensions UsageDimensions) (compiledModel, string, string, bool) {
	if r.snapshot == nil {
		return compiledModel{}, "", "", false
	}
	if model, ok := r.snapshot.modelsByName[dimensions.Model]; ok {
		return model, dimensions.Model, "model", true
	}
	if model, ok := r.snapshot.modelsByName[dimensions.ModelAlias]; ok {
		return model, dimensions.ModelAlias, "model_alias", true
	}
	return compiledModel{}, "", "", false
}

func matchingRuleMultiplier(rules []compiledRule, dimensions UsageDimensions) float64 {
	multiplier := 1.0
	for _, rule := range rules {
		if dimensions.Value(rule.field) != rule.value {
			continue
		}
		if rule.multiplier == 0 {
			return 0
		}
		multiplier *= rule.multiplier
	}
	return multiplier
}
