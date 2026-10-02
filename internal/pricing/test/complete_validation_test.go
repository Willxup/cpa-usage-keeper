package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestCompletePricingCompilerRejectsInvalidBoundsAndAmounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*pricing.ModelPricingConfig)
	}{
		{"negative default price", func(c *pricing.ModelPricingConfig) { c.BasePrices.Input = -1 }},
		{"nonfinite default price", func(c *pricing.ModelPricingConfig) { c.BasePrices.Input = math.NaN() }},
		{"nonfinite multiplier", func(c *pricing.ModelPricingConfig) { c.ModelMultiplier = math.Inf(1) }},
		{"negative branch price", func(c *pricing.ModelPricingConfig) { c.Branches[0].Prices.Input = -1 }},
		{"branch worst cost overflow", func(c *pricing.ModelPricingConfig) { c.Branches[0].Prices.Input = math.MaxFloat64 }},
		{"unsafe threshold", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Context.Threshold = int64Pointer(9_007_199_254_740_992)
		}},
		{"gt threshold plus one unsafe", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Context.Threshold = int64Pointer(9_007_199_254_740_991)
		}},
		{"negative threshold", func(c *pricing.ModelPricingConfig) { c.Branches[0].Context.Threshold = int64Pointer(-1) }},
		{"reversed range", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Context = pricing.ContextCondition{Type: pricing.ContextRange, Min: int64Pointer(20), Max: int64Pointer(10)}
		}},
		{"invalid clock", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("24:00"), End: stringPointer("08:00")}
		}},
		{"equal clock", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("08:00"), End: stringPointer("08:00")}
		}},
		{"invalid days", func(c *pricing.ModelPricingConfig) { c.Branches[0].Days = "holiday" }},
		{"weekday cross midnight", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Days = pricing.DaysWeekday
			c.Branches[0].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("20:00"), End: stringPointer("08:00")}
		}},
		{"weekend cross midnight", func(c *pricing.ModelPricingConfig) {
			c.Branches[0].Days = pricing.DaysWeekend
			c.Branches[0].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("20:00"), End: stringPointer("08:00")}
		}},
		{"duplicate branch ID", func(c *pricing.ModelPricingConfig) {
			c.Branches = append(c.Branches, branch("branch", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2))
		}},
		{"all conflicts with any branch", func(c *pricing.ModelPricingConfig) {
			c.Branches = append(c.Branches, branch("other", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := completePricing("model")
			config.Branches = []pricing.PriceBranch{branch("branch", pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(10)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2)}
			tc.mutate(&config)
			if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err == nil {
				t.Fatal("invalid complete pricing was accepted")
			}
		})
	}
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{completePricing("model")}, nil); err == nil {
		t.Fatal("deployment timezone is required")
	}
}

func TestCompletePricingCompilerAllowsDateOnlyBranch(t *testing.T) {
	config := completePricing("model")
	item := branch("weekday", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2)
	item.Days = pricing.DaysWeekday
	config.Branches = []pricing.PriceBranch{item}
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err != nil {
		t.Fatal(err)
	}
	item.Days = pricing.DaysWeekend
	config.Branches = append(config.Branches, item)
	config.Branches[1].ID = "weekend"
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err != nil {
		t.Fatal(err)
	}
	config.Branches[1].Days = pricing.DaysWeekday
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err == nil {
		t.Fatal("same weekday conditions should conflict")
	}
	config.Branches[1].Days = pricing.DaysAll
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err == nil {
		t.Fatal("all days and weekday should conflict")
	}
}

func TestCompleteResolverKeepsAliasPriorityAndNineRuleFields(t *testing.T) {
	primary := completePricing("primary")
	primary.BasePrices.Input = 3
	alias := completePricing("alias")
	alias.BasePrices.Input = 1
	alias.ConditionalMultipliers = []pricing.RuleConfig{
		{Key: " API_GROUP_KEY ", Value: "group", Multiplier: 2},
		{Key: "model", Value: "missing", Multiplier: 2},
		{Key: "auth_index", Value: "auth", Multiplier: 2},
		{Key: "model_alias", Value: "alias", Multiplier: 2},
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "response_service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2},
		{Key: "endpoint", Value: "/v1/responses", Multiplier: 2},
		{Key: "executor_type", Value: "openai", Multiplier: 2},
	}
	resolver := pricing.NewCatalog(mustPricingSnapshot(t, "UTC", primary, alias)).NewResolver()
	dimensions := pricing.UsageDimensions{
		APIGroupKey: "group", Model: "missing", AuthIndex: "auth", ModelAlias: "alias", ServiceTier: "priority",
		ResponseServiceTier: "priority", ReasoningEffort: "xhigh", Endpoint: "/v1/responses", ExecutorType: "openai",
	}
	subject := pricing.NewCostSubject(dimensions, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	assertFee(t, resolver.CalculateFee(subject), 512, true)
	subject = pricing.NewCostSubject(pricing.UsageDimensions{Model: "primary", ModelAlias: "alias"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	assertFee(t, resolver.CalculateFee(subject), 3, true)
}

func TestCompleteResolverDoesNotAddCacheOrOutputToContextThreshold(t *testing.T) {
	config := completePricing("model")
	config.Branches = []pricing.PriceBranch{branch("large", pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(200_000)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2)}
	config.Branches[0].Prices.Output = 4
	config.Branches[0].Prices.CacheRead = 0.5
	config.Branches[0].Prices.CacheWrite = 1
	resolver := pricing.NewCatalog(mustPricingSnapshot(t, "UTC", config)).NewResolver()
	// input_tokens 已由事件解码归一化；缓存和输出不再加入分支判断。
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{
		InputTokens: 200_000, OutputTokens: 1_000_000, CacheReadTokens: 50_000,
	})
	assertFee(t, resolver.CalculateFee(subject), 0.15, true)
	subject = pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{
		InputTokens: 210_000, OutputTokens: 1_000_000, CacheReadTokens: 10_000, CacheCreationTokens: 5_000,
	})
	assertFee(t, resolver.CalculateFee(subject), 4.4, true)
}
