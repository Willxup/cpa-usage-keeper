package helper

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestCalculateUsageTokenCostDoesNotDoubleChargeCachedTokens(t *testing.T) {
	pricing := entities.ModelPriceSetting{PromptPricePer1M: 3, CompletionPricePer1M: 15, CachePricePer1M: 0.3}
	cost := CalculateUsageTokenCost(UsageTokenCostInput{InputTokens: 1_000_000, OutputTokens: 500_000, CachedTokens: 200_000}, pricing)
	want := 0.8*3 + 0.5*15 + 0.2*0.3
	if cost != want {
		t.Fatalf("expected cost %.2f, got %.2f", want, cost)
	}
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
}

func TestCalculateUsageTokenCostAddsPerRequestPricing(t *testing.T) {
	pricing := entities.ModelPriceSetting{PricePerRequest: 0.063}
	cost := CalculateUsageTokenCost(UsageTokenCostInput{Requests: 3}, pricing)
	want := 0.189
	if cost != want {
		t.Fatalf("expected per-request cost %.3f, got %.3f", want, cost)
	}
}
