package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

func assertCostClose(t *testing.T, got, want float64) {
	t.Helper()
	if !(math.Abs(got-want) <= 0.0000001) {
		t.Fatalf("expected cost %.8f, got %.8f", want, got)
	}
}

func TestCalculateUsageTokenCostChargesFourTokenSegments(t *testing.T) {
	for _, pricingStyle := range []string{entities.ModelPricingStyleOpenAI, entities.ModelPricingStyleClaude} {
		t.Run(pricingStyle, func(t *testing.T) {
			pricing := entities.ModelPriceSetting{
				PricingStyle:         pricingStyle,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				CacheWritePricePer1M: 3.75,
			}
			cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
				InputTokens:         1_000_000,
				OutputTokens:        500_000,
				CacheReadTokens:     200_000,
				CacheCreationTokens: 100_000,
			}, pricing)
			assertCostClose(t, cost, 0.7*3+0.2*0.3+0.1*3.75+0.5*15)
		})
	}
}

func TestCalculateUsageTokenCostKeepsZeroWriteCost(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens:     1_000_000,
		CacheReadTokens: 200_000,
	}, pricing)
	assertCostClose(t, cost, 0.8*3+0.2*0.3)
}

func TestCalculateUsageTokenCostClampsNormalInputAtZero(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens:         100_000,
		CacheReadTokens:     80_000,
		CacheCreationTokens: 40_000,
	}, pricing)
	assertCostClose(t, cost, 0.08*0.3+0.04*3.75)
}

func TestCalculateUsageTokenCostDoesNotUnderflowWithHugeCacheTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3}
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens: 1, CacheReadTokens: math.MaxInt64, CacheCreationTokens: math.MaxInt64,
	}, pricing)
	if cost != 0 {
		t.Fatalf("cache subtraction underflowed into billable input: %v", cost)
	}
}

func TestCalculateUsageTokenCostAppliesMultiplierToEverySegment(t *testing.T) {
	multiplier := 1.5
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CacheReadPricePer1M:  1,
		CacheWritePricePer1M: 12.5,
		PriceMultiplier:      &multiplier,
	}
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing)
	assertCostClose(t, cost, (1.0*10+0.2*1+0.1*12.5+0.5*20)*1.5)
}

func TestCalculateUsageTokenCostReturnsFiniteZeroBeforeOverflowWhenMultiplierIsZero(t *testing.T) {
	zero := 0.0
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens:         math.MaxInt64,
		OutputTokens:        math.MaxInt64,
		CacheReadTokens:     math.MaxInt64,
		CacheCreationTokens: math.MaxInt64,
	}, entities.ModelPriceSetting{
		PromptPricePer1M:     math.MaxFloat64,
		CompletionPricePer1M: math.MaxFloat64,
		CacheReadPricePer1M:  math.MaxFloat64,
		CacheWritePricePer1M: math.MaxFloat64,
		PriceMultiplier:      &zero,
	})

	if cost != 0 {
		t.Fatalf("cost = %v, want finite zero", cost)
	}
}

func TestCalculateUsageTokenCostClampsNegativeTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	cost := helper.CalculateUsageTokenCost(helper.UsageTokenCostInput{
		InputTokens:         -1,
		OutputTokens:        -1,
		CacheReadTokens:     -1,
		CacheCreationTokens: -1,
	}, pricing)

	assertCostClose(t, cost, 0)
}

func TestUsageTokenInputRequiresPricingUsesCanonicalTokenFields(t *testing.T) {
	if helper.UsageTokenInputRequiresPricing(helper.UsageTokenCostInput{}) {
		t.Fatal("expected empty token input to not require pricing")
	}
	for name, input := range map[string]helper.UsageTokenCostInput{
		"input":       {InputTokens: 1},
		"output":      {OutputTokens: 1},
		"cache read":  {CacheReadTokens: 1},
		"cache write": {CacheCreationTokens: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if !helper.UsageTokenInputRequiresPricing(input) {
				t.Fatalf("expected %s tokens to require pricing", name)
			}
		})
	}
}
