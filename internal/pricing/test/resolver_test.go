package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestResolverPrefersModelThenFallsBackToAlias(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t,
		pricing.ModelConfig{Pricing: testPricingWithPrompt("base-model", 10)},
		pricing.ModelConfig{Pricing: testPricingWithPrompt("alias-model", 2)},
	)
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: " base-model ", ModelAlias: " alias-model "}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	result := resolver.CalculateFee(subject)
	assertResultCost(t, result, 10)

	result = resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing", ModelAlias: "alias-model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 2)
}

func TestResolverPreservesMissingPriceAvailabilityContract(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t)
	billable := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{InputTokens: 1}))
	if billable.Available || billable.TotalCostUSD != 0 {
		t.Fatalf("expected missing billable price to be unavailable, got %+v", billable)
	}
	empty := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{}))
	if !empty.Available || empty.TotalCostUSD != 0 {
		t.Fatalf("expected missing zero-token price to be available, got %+v", empty)
	}
}

func TestResolverWithoutRulesPreservesFourTokenTotalAndModelMultiplier(t *testing.T) {
	t.Parallel()

	one := 1.0
	zero := 0.0
	tokens := helper.UsageTokenCostInput{
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}
	for _, testCase := range []struct {
		name       string
		multiplier *float64
		dimensions pricing.UsageDimensions
	}{
		{name: "nil multiplier direct model", dimensions: pricing.UsageDimensions{Model: "priced-model"}},
		{name: "one multiplier direct model", multiplier: &one, dimensions: pricing.UsageDimensions{Model: "priced-model", ModelAlias: "alias-model"}},
		{name: "nil multiplier alias fallback", dimensions: pricing.UsageDimensions{Model: "missing-model", ModelAlias: "priced-model"}},
		{name: "zero multiplier", multiplier: &zero, dimensions: pricing.UsageDimensions{Model: "priced-model"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			setting := entities.ModelPriceSetting{
				Model:                "priced-model",
				PricingStyle:         entities.ModelPricingStyleOpenAI,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				CacheWritePricePer1M: 3.75,
				PriceMultiplier:      testCase.multiplier,
			}
			resolver := compileResolver(t, pricing.ModelConfig{Pricing: setting})
			result := resolver.CalculateFee(pricing.NewCostSubject(testCase.dimensions, tokens))
			if !result.Available {
				t.Fatalf("unexpected no-Rules match result: %+v", result)
			}
			want := 0.7*3 + 0.2*0.3 + 0.1*3.75 + 0.5*15
			if testCase.multiplier != nil {
				want *= *testCase.multiplier
			}
			assertResultCost(t, result, want)
		})
	}
}

func TestResolverMultipliesEveryMatchingRuleContinuously(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPromptAndMultiplier("model-a", 10, 1.5),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
			{Key: "endpoint", Value: "/v1/responses", Multiplier: 4},
		},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{
		Model:           "model-a",
		ServiceTier:     "priority",
		ReasoningEffort: "xhigh",
		Endpoint:        "/v1/responses",
	}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))

	assertResultCost(t, result, 10*1.5*2*3*4)
}

func TestResolverMatchesValuesExactlyAndCaseSensitively(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules:   []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "Priority"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 10)
}

func TestResolverSupportsAllNineRuleFields(t *testing.T) {
	t.Parallel()

	rules := []pricing.RuleConfig{
		{Key: "api_group_key", Value: "group", Multiplier: 2},
		{Key: "model", Value: "model-a", Multiplier: 2},
		{Key: "auth_index", Value: "auth", Multiplier: 2},
		{Key: "model_alias", Value: "alias", Multiplier: 2},
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "response_service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2},
		{Key: "endpoint", Value: "/v1/responses", Multiplier: 2},
		{Key: "executor_type", Value: "openai", Multiplier: 2},
	}
	resolver := compileResolver(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 1), Rules: rules})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{
		APIGroupKey:         "group",
		Model:               "model-a",
		AuthIndex:           "auth",
		ModelAlias:          "alias",
		ServiceTier:         "priority",
		ResponseServiceTier: "priority",
		ReasoningEffort:     "xhigh",
		Endpoint:            "/v1/responses",
		ExecutorType:        "openai",
	}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))

	assertResultCost(t, result, 512)
}

func TestResolverTreatsZeroAsAvailableAndOneAsInactive(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules: []pricing.RuleConfig{
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 1},
			{Key: "service_tier", Value: "priority", Multiplier: 0},
		},
	})
	result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.TotalCostUSD != 0 {
		t.Fatalf("expected matched zero rule to return available zero cost, got %+v", result)
	}
}

func TestResolverZeroRuleResultDoesNotDependOnRuleOrder(t *testing.T) {
	t.Parallel()

	rules := []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 0},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 1e100},
	}
	for _, ordered := range [][]pricing.RuleConfig{rules, {rules[1], rules[0]}} {
		resolver := compileResolver(t, pricing.ModelConfig{
			Pricing: testPricingWithPrompt("model-a", 1e-100),
			Rules:   ordered,
		})
		result := resolver.CalculateFee(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: math.MaxInt64}))
		if !result.Available || result.TotalCostUSD != 0 {
			t.Fatalf("expected finite zero result for rules %+v, got %+v", ordered, result)
		}
	}
}

func TestResolverCalculateFeeHasNoHeapAllocations(t *testing.T) {
	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
		},
	})
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	allocations := testing.AllocsPerRun(1000, func() {
		_ = resolver.CalculateFee(subject)
	})
	if allocations != 0 {
		t.Fatalf("expected zero allocations, got %.2f", allocations)
	}
}

func compileSnapshot(t testing.TB, configs ...pricing.ModelConfig) *pricing.Snapshot {
	t.Helper()
	snapshot, err := pricing.CompileSnapshot(configs)
	if err != nil {
		t.Fatalf("CompileSnapshot returned error: %v", err)
	}
	return snapshot
}

func compileResolver(t *testing.T, configs ...pricing.ModelConfig) pricing.Resolver {
	t.Helper()
	return pricing.NewCatalog(compileSnapshot(t, configs...)).NewResolver()
}

func testPricingWithPrompt(model string, prompt float64) entities.ModelPriceSetting {
	return testPricingWithPromptAndMultiplier(model, prompt, 1)
}

func testPricingWithPromptAndMultiplier(model string, prompt, multiplier float64) entities.ModelPriceSetting {
	return entities.ModelPriceSetting{
		Model:            model,
		PricingStyle:     entities.ModelPricingStyleOpenAI,
		PromptPricePer1M: prompt,
		PriceMultiplier:  &multiplier,
	}
}

func assertResultCost(t *testing.T, result pricing.FeeResult, want float64) {
	t.Helper()
	if !result.Available {
		t.Fatalf("expected available cost, got %+v", result)
	}
	if !(math.Abs(result.TotalCostUSD-want) <= math.Max(1e-9, math.Abs(want)*1e-12)) {
		t.Fatalf("cost = %.12f, want %.12f", result.TotalCostUSD, want)
	}
}
