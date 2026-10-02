package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
)

func TestSnapshotPricingStyleForModelUsesCurrentModelThenAlias(t *testing.T) {
	primary := testPricingWithPrompt("primary", 10)
	primary.PricingStyle = entities.ModelPricingStyleClaude
	alias := testPricingWithPrompt("alias", 2)
	unknown := testPricingWithPrompt("unknown", 1)
	unknown.PricingStyle = entities.ModelPricingStyleClaude
	snapshot := compileSnapshot(t, pricing.ModelConfig{Pricing: primary}, pricing.ModelConfig{Pricing: alias}, pricing.ModelConfig{Pricing: unknown})
	for _, test := range []struct {
		model, alias, want string
	}{
		{" primary ", " alias ", entities.ModelPricingStyleClaude},
		{"  ", " alias ", entities.ModelPricingStyleClaude},
		{"missing", " alias ", entities.ModelPricingStyleOpenAI},
		{"missing", "also-missing", ""},
	} {
		if got := snapshot.PricingStyleForModel(test.model, test.alias); got != test.want {
			t.Errorf("PricingStyleForModel(%q, %q) = %q, want %q", test.model, test.alias, got, test.want)
		}
	}
	var absent *pricing.Snapshot
	if got := absent.PricingStyleForModel("primary", "alias"); got != "" {
		t.Fatalf("nil snapshot style = %q", got)
	}
}

func TestCompleteSnapshotNormalizesRulesAndKeepsVisibleIdentityRules(t *testing.T) {
	t.Parallel()
	model := completePricing(" model-b ")
	model.ModelMultiplier = 2
	model.ConditionalMultipliers = []pricing.RuleConfig{
		{Key: " SERVICE_TIER ", Value: " priority ", Multiplier: 2},
		{Key: "reasoning_effort", Value: " xhigh ", Multiplier: 1},
	}
	snapshot := mustPricingSnapshot(t, "UTC", model, completePricing("model-a"))
	configs := snapshot.PricingModelConfigs()
	if len(configs) != 2 || configs[0].Model != "model-a" || configs[1].Model != "model-b" {
		t.Fatalf("expected stable model-sorted configs, got %+v", configs)
	}
	rule := configs[1].ConditionalMultipliers[0]
	if rule.Key != "service_tier" || rule.Value != "priority" || rule.Multiplier != 2 {
		t.Fatalf("expected normalized rule, got %+v", rule)
	}
	if len(configs[1].ConditionalMultipliers) != 2 || configs[1].ConditionalMultipliers[1].Multiplier != 1 {
		t.Fatalf("expected multiplier-1 rule to remain visible, got %+v", configs[1].ConditionalMultipliers)
	}
}

func TestCompleteSnapshotDefensivelyCopiesInputsAndListOutputs(t *testing.T) {
	t.Parallel()
	config := completePricing("model-a")
	config.BasePrices.Input = 3
	config.ModelMultiplier = 2
	config.ConditionalMultipliers = []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}}
	configs := []pricing.ModelPricingConfig{config}
	snapshot := mustPricingSnapshot(t, "UTC", configs...)
	configs[0].Model = "mutated"
	configs[0].ModelMultiplier = 100
	configs[0].ConditionalMultipliers[0].Value = "mutated"
	first := snapshot.PricingModelConfigs()
	first[0].Model = "changed-output"
	first[0].ModelMultiplier = 200
	first[0].ConditionalMultipliers[0].Value = "changed-output"
	second := snapshot.PricingModelConfigs()
	if second[0].Model != "model-a" || second[0].ModelMultiplier != 2 || second[0].ConditionalMultipliers[0].Value != "priority" {
		t.Fatalf("snapshot exposed mutable state: %+v", second)
	}
}

func TestCompileSnapshotRejectsInvalidModelsPricesAndRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		configs []pricing.ModelConfig
	}{
		{"blank model", []pricing.ModelConfig{{Pricing: testPricing(" ", 1)}}},
		{"duplicate model", []pricing.ModelConfig{{Pricing: testPricing("same", 1)}, {Pricing: testPricing(" same ", 1)}}},
		{"negative price", []pricing.ModelConfig{{Pricing: entities.ModelPriceSetting{Model: "model", PromptPricePer1M: -1}}}},
		{"nan price", []pricing.ModelConfig{{Pricing: entities.ModelPriceSetting{Model: "model", PromptPricePer1M: math.NaN()}}}},
		{"negative model multiplier", []pricing.ModelConfig{{Pricing: testPricing("model", -1)}}},
		{"infinite model multiplier", []pricing.ModelConfig{{Pricing: testPricing("model", math.Inf(1))}}},
		{"unknown key", []pricing.ModelConfig{{Pricing: testPricing("model", 1), Rules: []pricing.RuleConfig{{Key: "provider", Value: "openai", Multiplier: 2}}}}},
		{"blank value", []pricing.ModelConfig{{Pricing: testPricing("model", 1), Rules: []pricing.RuleConfig{{Key: "service_tier", Value: " ", Multiplier: 2}}}}},
		{"negative rule multiplier", []pricing.ModelConfig{{Pricing: testPricing("model", 1), Rules: []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: -1}}}}},
		{"nan rule multiplier", []pricing.ModelConfig{{Pricing: testPricing("model", 1), Rules: []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: math.NaN()}}}}},
		{"duplicate normalized rule", []pricing.ModelConfig{{Pricing: testPricing("model", 1), Rules: []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}, {Key: " SERVICE_TIER ", Value: " priority ", Multiplier: 3}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := pricing.CompileSnapshot(tt.configs); err == nil {
				t.Fatalf("expected CompileSnapshot to reject %s", tt.name)
			}
		})
	}
}

func TestCompileSnapshotRejectsFiniteInputsWhoseWorstCaseCostOverflows(t *testing.T) {
	t.Parallel()

	for _, config := range []pricing.ModelConfig{
		{Pricing: entities.ModelPriceSetting{Model: "huge-price", PromptPricePer1M: math.MaxFloat64}},
		{
			Pricing: testPricing("huge-rule-product", 1),
			Rules: []pricing.RuleConfig{
				{Key: "service_tier", Value: "priority", Multiplier: math.MaxFloat64 / 2},
				{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2},
			},
		},
	} {
		if _, err := pricing.CompileSnapshot([]pricing.ModelConfig{config}); err == nil {
			t.Fatalf("expected overflow configuration to fail: %+v", config)
		}
	}
}

func TestCompileSnapshotRejectsUnscaledSegmentSumOverflowHiddenBySmallMultiplier(t *testing.T) {
	t.Parallel()

	maxTokensPerMillion := float64(math.MaxInt64) / 1_000_000
	segmentPrice := (math.MaxFloat64 * 0.6) / maxTokensPerMillion
	multiplier := 0.25
	config := pricing.ModelConfig{Pricing: entities.ModelPriceSetting{
		Model:                "hidden-overflow",
		PromptPricePer1M:     segmentPrice,
		CompletionPricePer1M: segmentPrice,
		PriceMultiplier:      &multiplier,
	}}

	if _, err := pricing.CompileSnapshot([]pricing.ModelConfig{config}); err == nil {
		t.Fatal("expected the unscaled input and output sum overflow to be rejected")
	}
}

func TestCompileSnapshotAllowsZeroModelMultiplierWithHugeFiniteRules(t *testing.T) {
	t.Parallel()

	config := pricing.ModelConfig{
		Pricing: testPricing("free-model", 0),
		Rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: math.MaxFloat64},
			{Key: "reasoning_effort", Value: "xhigh", Multiplier: math.MaxFloat64},
		},
	}
	if _, err := pricing.CompileSnapshot([]pricing.ModelConfig{config}); err != nil {
		t.Fatalf("zero model multiplier must keep final cost finite: %v", err)
	}
}

func testPricing(model string, multiplier float64) entities.ModelPriceSetting {
	return entities.ModelPriceSetting{
		Model:                model,
		PricingStyle:         entities.ModelPricingStyleOpenAI,
		PromptPricePer1M:     1,
		CompletionPricePer1M: 2,
		CacheReadPricePer1M:  0.1,
		CacheWritePricePer1M: 1.25,
		PriceMultiplier:      &multiplier,
	}
}
