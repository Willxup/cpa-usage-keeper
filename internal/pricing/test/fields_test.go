package test

import (
	"testing"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestParseRuleFieldAcceptsOnlyCanonicalPricingDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  pricing.RuleField
	}{
		{" API_GROUP_KEY ", pricing.RuleFieldAPIGroupKey},
		{"MODEL", pricing.RuleFieldModel},
		{"auth_index", pricing.RuleFieldAuthIndex},
		{"model_alias", pricing.RuleFieldModelAlias},
		{"service_tier", pricing.RuleFieldServiceTier},
		{"response_service_tier", pricing.RuleFieldResponseServiceTier},
		{"reasoning_effort", pricing.RuleFieldReasoningEffort},
		{"endpoint", pricing.RuleFieldEndpoint},
		{"executor_type", pricing.RuleFieldExecutorType},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := pricing.ParseRuleField(tt.input)
			if err != nil {
				t.Fatalf("ParseRuleField returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseRuleField(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if got.String() == "" {
				t.Fatalf("expected canonical field name for %v", got)
			}
		})
	}

	for _, input := range []string{"", "provider", "service-tier"} {
		if _, err := pricing.ParseRuleField(input); err == nil {
			t.Fatalf("expected ParseRuleField(%q) to fail", input)
		}
	}
}

func TestNewCostSubjectCanonicalizesEveryPricingDimension(t *testing.T) {
	t.Parallel()

	subject := pricing.NewCostSubject(pricing.UsageDimensions{
		APIGroupKey:         "   ",
		Model:               "  ",
		AuthIndex:           " auth-1 ",
		ModelAlias:          " alias-1 ",
		ServiceTier:         " priority ",
		ResponseServiceTier: " default ",
		ReasoningEffort:     " xhigh ",
		Endpoint:            " /v1/responses ",
		ExecutorType:        " openai ",
	}, helper.UsageTokenCostInput{InputTokens: 42})

	wants := map[pricing.RuleField]string{
		pricing.RuleFieldAPIGroupKey:         "unknown",
		pricing.RuleFieldModel:               "unknown",
		pricing.RuleFieldAuthIndex:           "auth-1",
		pricing.RuleFieldModelAlias:          "alias-1",
		pricing.RuleFieldServiceTier:         "priority",
		pricing.RuleFieldResponseServiceTier: "default",
		pricing.RuleFieldReasoningEffort:     "xhigh",
		pricing.RuleFieldEndpoint:            "/v1/responses",
		pricing.RuleFieldExecutorType:        "openai",
	}
	for field, want := range wants {
		if got := subject.Dimensions.Value(field); got != want {
			t.Errorf("Value(%s) = %q, want %q", field, got, want)
		}
	}
	if subject.Tokens.InputTokens != 42 {
		t.Fatalf("expected token input to be preserved, got %+v", subject.Tokens)
	}
}

func TestNewCostSubjectKeepsOptionalBlankDimensionsEmpty(t *testing.T) {
	t.Parallel()

	subject := pricing.NewCostSubject(pricing.UsageDimensions{}, helper.UsageTokenCostInput{})
	for _, field := range []pricing.RuleField{
		pricing.RuleFieldAuthIndex,
		pricing.RuleFieldModelAlias,
		pricing.RuleFieldServiceTier,
		pricing.RuleFieldResponseServiceTier,
		pricing.RuleFieldReasoningEffort,
		pricing.RuleFieldEndpoint,
		pricing.RuleFieldExecutorType,
	} {
		if got := subject.Dimensions.Value(field); got != "" {
			t.Errorf("Value(%s) = %q, want empty", field, got)
		}
	}
}
