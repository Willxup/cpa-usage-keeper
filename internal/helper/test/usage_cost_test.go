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

func TestCalculateUsageTokenCostBreakdownDoesNotDoubleChargeCachedTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	cost := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing).TotalCostUSD
	want := 0.8*3 + 0.5*15 + 0.2*0.3
	assertCostClose(t, cost, want)
}

func TestCalculateUsageTokenCostBreakdownSplitsOpenAIStyleCachedInput(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing)

	assertCostClose(t, breakdown.InputCostUSD, 0.8*3)
	assertCostClose(t, breakdown.OutputCostUSD, 0.5*15)
	assertCostClose(t, breakdown.CachedCostUSD, 0.2*0.3)
	assertCostClose(t, breakdown.TotalCostUSD, 0.8*3+0.5*15+0.2*0.3)
}

func TestCalculateUsageTokenCostBreakdownAppliesOpenAIStyleMultiplier(t *testing.T) {
	multiplier := 2.0
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3, PriceMultiplier: &multiplier}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing)

	assertCostClose(t, breakdown.InputCostUSD, 0.8*3*2)
	assertCostClose(t, breakdown.OutputCostUSD, 0.5*15*2)
	assertCostClose(t, breakdown.CachedCostUSD, 0.2*0.3*2)
	assertCostClose(t, breakdown.TotalCostUSD, (0.8*3+0.5*15+0.2*0.3)*2)
}

func TestCalculateUsageTokenCostBreakdownChargesClaudeCacheReadAndCreationSeparately(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
	}
	cost := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing).TotalCostUSD
	want := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	assertCostClose(t, cost, want)
}

func TestCalculateUsageTokenCostBreakdownGroupsClaudeCacheReadAndCreationAsCachedCost(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing)

	assertCostClose(t, breakdown.InputCostUSD, 1.0*10)
	assertCostClose(t, breakdown.OutputCostUSD, 0.5*20)
	assertCostClose(t, breakdown.CachedCostUSD, 0.2*1+0.1*12.5)
	assertCostClose(t, breakdown.TotalCostUSD, 1.0*10+0.5*20+0.2*1+0.1*12.5)
}

func TestCalculateUsageTokenCostBreakdownAppliesClaudeMultiplier(t *testing.T) {
	multiplier := 1.5
	pricing := entities.ModelPriceSetting{
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
		PriceMultiplier:         &multiplier,
	}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing)

	assertCostClose(t, breakdown.InputCostUSD, 1.0*10*1.5)
	assertCostClose(t, breakdown.OutputCostUSD, 0.5*20*1.5)
	assertCostClose(t, breakdown.CachedCostUSD, (0.2*1+0.1*12.5)*1.5)
	assertCostClose(t, breakdown.TotalCostUSD, (1.0*10+0.5*20+0.2*1+0.1*12.5)*1.5)
}

func TestCalculateUsageTokenCostBreakdownPreservesExplicitZeroMultiplier(t *testing.T) {
	zero := 0.0
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3, PriceMultiplier: &zero}
	breakdown := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing)

	assertCostClose(t, breakdown.InputCostUSD, 0)
	assertCostClose(t, breakdown.OutputCostUSD, 0)
	assertCostClose(t, breakdown.CachedCostUSD, 0)
	assertCostClose(t, breakdown.TotalCostUSD, 0)
}

func TestCalculateUsageTokenCostBreakdownClampsNegativeTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	cost := helper.CalculateUsageTokenCostBreakdown(helper.UsageTokenCostInput{InputTokens: -1, OutputTokens: -1, CachedTokens: -1}, pricing).TotalCostUSD
	if cost != 0 {
		t.Fatalf("expected negative tokens to cost 0, got %.2f", cost)
	}
}

func TestUsageTokenInputRequiresPricingUsesBillableTokenFields(t *testing.T) {
	if helper.UsageTokenInputRequiresPricing(helper.UsageTokenCostInput{}) {
		t.Fatal("expected empty token input to not require pricing")
	}
	if !helper.UsageTokenInputRequiresPricing(helper.UsageTokenCostInput{InputTokens: 1}) {
		t.Fatal("expected input tokens to require pricing")
	}
	if !helper.UsageTokenInputRequiresPricing(helper.UsageTokenCostInput{CacheReadTokens: 1}) {
		t.Fatal("expected cache read tokens to require pricing")
	}
	if !helper.UsageTokenInputRequiresPricing(helper.UsageTokenCostInput{CacheCreationTokens: 1}) {
		t.Fatal("expected cache creation tokens to require pricing")
	}
}
