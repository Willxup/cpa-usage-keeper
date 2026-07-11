package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

func assertCostClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("expected cost %.8f, got %.8f", want, got)
	}
}

func TestCalculateUsageTokenCostBreakdownChargesFourTokenSegments(t *testing.T) {
	for _, pricingStyle := range []string{entities.ModelPricingStyleOpenAI, entities.ModelPricingStyleClaude} {
		t.Run(pricingStyle, func(t *testing.T) {
			pricing := entities.ModelPriceSetting{
				PricingStyle:         pricingStyle,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				CacheWritePricePer1M: 3.75,
			}
			breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
				InputTokens:         1_000_000,
				OutputTokens:        500_000,
				CacheReadTokens:     200_000,
				CacheCreationTokens: 100_000,
			}, pricing)

			assertCostClose(t, breakdown.UncachedInputCostUSD, 0.7*3)
			assertCostClose(t, breakdown.CacheReadCostUSD, 0.2*0.3)
			assertCostClose(t, breakdown.CacheWriteCostUSD, 0.1*3.75)
			assertCostClose(t, breakdown.OutputCostUSD, 0.5*15)
			assertCostClose(t, breakdown.TotalCostUSD, 0.7*3+0.2*0.3+0.1*3.75+0.5*15)
		})
	}
}

func TestCalculateUsageTokenCostBreakdownKeepsZeroWriteCost(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:     1_000_000,
		CacheReadTokens: 200_000,
	}, pricing)

	assertCostClose(t, breakdown.UncachedInputCostUSD, 0.8*3)
	assertCostClose(t, breakdown.CacheReadCostUSD, 0.2*0.3)
	assertCostClose(t, breakdown.CacheWriteCostUSD, 0)
}

func TestCalculateUsageTokenCostBreakdownClampsNormalInputAtZero(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         100_000,
		CacheReadTokens:     80_000,
		CacheCreationTokens: 40_000,
	}, pricing)

	assertCostClose(t, breakdown.UncachedInputCostUSD, 0)
	assertCostClose(t, breakdown.CacheReadCostUSD, 0.08*0.3)
	assertCostClose(t, breakdown.CacheWriteCostUSD, 0.04*3.75)
}

func TestCalculateUsageTokenCostBreakdownAppliesMultiplierToEverySegment(t *testing.T) {
	multiplier := 1.5
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CacheReadPricePer1M:  1,
		CacheWritePricePer1M: 12.5,
		PriceMultiplier:      &multiplier,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing)

	assertCostClose(t, breakdown.UncachedInputCostUSD, 1.0*10*1.5)
	assertCostClose(t, breakdown.CacheReadCostUSD, 0.2*1*1.5)
	assertCostClose(t, breakdown.CacheWriteCostUSD, 0.1*12.5*1.5)
	assertCostClose(t, breakdown.OutputCostUSD, 0.5*20*1.5)
	assertCostClose(t, breakdown.TotalCostUSD, (1.0*10+0.2*1+0.1*12.5+0.5*20)*1.5)
}

func TestCalculateUsageTokenCostBreakdownClampsNegativeTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         -1,
		OutputTokens:        -1,
		CacheReadTokens:     -1,
		CacheCreationTokens: -1,
	}, pricing)

	assertCostClose(t, breakdown.TotalCostUSD, 0)
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
