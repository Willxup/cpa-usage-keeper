package test

import (
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
)

func TestPricingDimensionColumnsUseOnlyCompiledFixedFields(t *testing.T) {
	resolver := pricingResolverForDimensionTest(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
		{Key: "model", Value: "model-a", Multiplier: 4},
	})
	got := repository.UsagePricingDimensionColumns(resolver.ActiveFields())
	want := []string{"model", "model_alias", "service_tier", "reasoning_effort"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UsagePricingDimensionColumns = %#v, want %#v", got, want)
	}

	noRules := pricingResolverForDimensionTest(t, nil)
	got = repository.UsagePricingDimensionColumns(noRules.ActiveFields())
	want = []string{"model", "model_alias"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-rule columns = %#v, want %#v", got, want)
	}
}

func TestPricingDimensionColumnsCoverAllAllowedRuleFields(t *testing.T) {
	rules := []pricing.RuleConfig{
		{Key: "api_group_key", Value: "a", Multiplier: 2},
		{Key: "model", Value: "m", Multiplier: 2},
		{Key: "auth_index", Value: "i", Multiplier: 2},
		{Key: "model_alias", Value: "a", Multiplier: 2},
		{Key: "service_tier", Value: "s", Multiplier: 2},
		{Key: "response_service_tier", Value: "r", Multiplier: 2},
		{Key: "reasoning_effort", Value: "e", Multiplier: 2},
		{Key: "endpoint", Value: "p", Multiplier: 2},
		{Key: "executor_type", Value: "x", Multiplier: 2},
	}
	resolver := pricingResolverForDimensionTest(t, rules)
	want := []string{"model", "model_alias", "api_group_key", "auth_index", "service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"}
	if got := repository.UsagePricingDimensionColumns(resolver.ActiveFields()); !reflect.DeepEqual(got, want) {
		t.Fatalf("all active columns = %#v, want %#v", got, want)
	}
}

func pricingResolverForDimensionTest(t *testing.T, rules []pricing.RuleConfig) pricing.Resolver {
	t.Helper()
	multiplier := 1.0
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{
		Pricing: entities.ModelPriceSetting{Model: "model-a", PromptPricePer1M: 1, PriceMultiplier: &multiplier},
		Rules:   rules,
	}})
	if err != nil {
		t.Fatalf("CompileSnapshot: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}
