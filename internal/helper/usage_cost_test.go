package helper

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func assertCostClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("expected cost %.8f, got %.8f", want, got)
	}
}

func TestCalculateUsageTokenCostDoesNotDoubleChargeCachedTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	cost := CalculateUsageTokenCost(UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing)
	want := 0.8*3 + 0.5*15 + 0.2*0.3
	assertCostClose(t, cost, want)
}

func TestCalculateUsageTokenCostChargesClaudeCacheReadAndCreationSeparately(t *testing.T) {
	pricing := entities.ModelPriceSetting{
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
	}
	cost := CalculateUsageTokenCost(UsageTokenCostInput{
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}, pricing)
	want := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	assertCostClose(t, cost, want)
}

func TestCalculateUsageTokenCostClampsNegativeTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	cost := CalculateUsageTokenCost(UsageTokenCostInput{InputTokens: -1, OutputTokens: -1, CachedTokens: -1}, pricing)
	if cost != 0 {
		t.Fatalf("expected negative tokens to cost 0, got %.2f", cost)
	}
}

func TestUsageEventRequiresPricingUsesBillableTokenFields(t *testing.T) {
	if UsageEventRequiresPricing(entities.UsageEvent{}) {
		t.Fatal("expected event without billable tokens to not require pricing")
	}
	if !UsageEventRequiresPricing(entities.UsageEvent{InputTokens: 1}) {
		t.Fatal("expected input tokens to require pricing")
	}
	if !UsageTokenInputRequiresPricing(UsageTokenCostInput{CacheReadTokens: 1}) {
		t.Fatal("expected cache read tokens to require pricing")
	}
	if !UsageTokenInputRequiresPricing(UsageTokenCostInput{CacheCreationTokens: 1}) {
		t.Fatal("expected cache creation tokens to require pricing")
	}
}
