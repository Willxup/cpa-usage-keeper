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
	result := resolver.Calculate(subject)
	assertResultCost(t, result, 10)
	if result.MatchedModel != "base-model" || result.MatchedBy != "model" {
		t.Fatalf("expected model match, got %+v", result)
	}

	result = resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing", ModelAlias: "alias-model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 2)
	if result.MatchedModel != "alias-model" || result.MatchedBy != "model_alias" {
		t.Fatalf("expected alias fallback, got %+v", result)
	}
}

func TestResolverPreservesMissingPriceAvailabilityContract(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t)
	billable := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{InputTokens: 1}))
	if billable.Available || billable.Cost.TotalCostUSD != 0 {
		t.Fatalf("expected missing billable price to be unavailable, got %+v", billable)
	}
	empty := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "missing"}, helper.UsageTokenCostInput{}))
	if !empty.Available || empty.Cost.TotalCostUSD != 0 {
		t.Fatalf("expected missing zero-token price to be available, got %+v", empty)
	}
}

func TestResolverWithoutRulesMatchesLegacyHelperForEveryTokenSegmentAndModelMultiplier(t *testing.T) {
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
		matchedBy  string
	}{
		{name: "nil multiplier direct model", dimensions: pricing.UsageDimensions{Model: "priced-model"}, matchedBy: "model"},
		{name: "one multiplier direct model", multiplier: &one, dimensions: pricing.UsageDimensions{Model: "priced-model", ModelAlias: "alias-model"}, matchedBy: "model"},
		{name: "nil multiplier alias fallback", dimensions: pricing.UsageDimensions{Model: "missing-model", ModelAlias: "priced-model"}, matchedBy: "model_alias"},
		{name: "zero multiplier", multiplier: &zero, dimensions: pricing.UsageDimensions{Model: "priced-model"}, matchedBy: "model"},
	} {
		testCase := testCase
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
			if resolver.ActiveFields() != 0 {
				t.Fatalf("no Rules must not activate extra grouping fields: %v", resolver.ActiveFields())
			}

			result := resolver.Calculate(pricing.NewCostSubject(testCase.dimensions, tokens))
			if !result.Available || result.MatchedBy != testCase.matchedBy || result.RuleMultiplier != 1 {
				t.Fatalf("unexpected no-Rules match result: %+v", result)
			}
			assertUsageCostBreakdownEqual(t, result.Cost, helper.CalculateUsageTokenCostBreakdown(tokens, setting))
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
	result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{
		Model:           "model-a",
		ServiceTier:     "priority",
		ReasoningEffort: "xhigh",
		Endpoint:        "/v1/responses",
	}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))

	assertResultCost(t, result, 10*1.5*2*3*4)
	if result.RuleMultiplier != 24 {
		t.Fatalf("expected rule multiplier 24, got %+v", result)
	}
}

func TestResolverMatchesValuesExactlyAndCaseSensitively(t *testing.T) {
	t.Parallel()

	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules:   []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}},
	})
	result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "Priority"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	assertResultCost(t, result, 10)
	if result.RuleMultiplier != 1 {
		t.Fatalf("expected case mismatch to keep multiplier 1, got %+v", result)
	}
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
	result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{
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
	if result.RuleMultiplier != 512 {
		t.Fatalf("expected all nine rules to multiply, got %+v", result)
	}
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
	if resolver.ActiveFields().Has(pricing.RuleFieldReasoningEffort) {
		t.Fatal("expected multiplier-1 field to be inactive")
	}
	if !resolver.ActiveFields().Has(pricing.RuleFieldServiceTier) {
		t.Fatal("expected multiplier-0 field to be active")
	}
	result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.RuleMultiplier != 0 || result.Cost.TotalCostUSD != 0 {
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
		result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: math.MaxInt64}))
		if !result.Available || result.RuleMultiplier != 0 || result.Cost.TotalCostUSD != 0 {
			t.Fatalf("expected finite zero result for rules %+v, got %+v", ordered, result)
		}
	}
}

func TestResolverCalculateHasNoHeapAllocations(t *testing.T) {
	resolver := compileResolver(t, pricing.ModelConfig{
		Pricing: testPricingWithPrompt("model-a", 10),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
		},
	})
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	allocations := testing.AllocsPerRun(1000, func() {
		_ = resolver.Calculate(subject)
	})
	if allocations != 0 {
		t.Fatalf("expected zero allocations, got %.2f", allocations)
	}
}

func compileResolver(t *testing.T, configs ...pricing.ModelConfig) pricing.Resolver {
	t.Helper()
	snapshot, err := pricing.CompileSnapshot(configs)
	if err != nil {
		t.Fatalf("CompileSnapshot returned error: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}

func testPricingWithPrompt(model string, prompt float64) entities.ModelPriceSetting {
	return testPricingWithPromptAndMultiplier(model, prompt, 1)
}

func testPricingWithPromptAndMultiplier(model string, prompt, multiplier float64) entities.ModelPriceSetting {
	pricingSetting := testPricing(model, multiplier)
	pricingSetting.PromptPricePer1M = prompt
	pricingSetting.CompletionPricePer1M = 0
	pricingSetting.CacheReadPricePer1M = 0
	pricingSetting.CacheWritePricePer1M = 0
	return pricingSetting
}

func assertResultCost(t *testing.T, result pricing.CostResult, want float64) {
	t.Helper()
	if !result.Available {
		t.Fatalf("expected available cost, got %+v", result)
	}
	if math.Abs(result.Cost.TotalCostUSD-want) > math.Max(1e-9, math.Abs(want)*1e-12) {
		t.Fatalf("cost = %.12f, want %.12f", result.Cost.TotalCostUSD, want)
	}
}

func assertUsageCostBreakdownEqual(t *testing.T, got, want helper.UsageTokenCostBreakdown) {
	t.Helper()
	for name, pair := range map[string][2]float64{
		"uncached input": {got.UncachedInputCostUSD, want.UncachedInputCostUSD},
		"cache read":     {got.CacheReadCostUSD, want.CacheReadCostUSD},
		"cache write":    {got.CacheWriteCostUSD, want.CacheWriteCostUSD},
		"output":         {got.OutputCostUSD, want.OutputCostUSD},
		"total":          {got.TotalCostUSD, want.TotalCostUSD},
	} {
		if math.Abs(pair[0]-pair[1]) > math.Max(1e-9, math.Abs(pair[1])*1e-12) {
			t.Fatalf("%s cost = %.12f, want %.12f", name, pair[0], pair[1])
		}
	}
}
