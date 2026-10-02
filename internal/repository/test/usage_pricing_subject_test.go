package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageEventPricingSubjectMapsAllNineFieldsAndFourTokenSegments(t *testing.T) {
	alias := " alias-a "
	event := entities.UsageEvent{
		Timestamp:           time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC),
		APIGroupKey:         " group-a ",
		Model:               " model-a ",
		AuthIndex:           " auth-a ",
		ModelAlias:          &alias,
		ServiceTier:         " priority ",
		ResponseServiceTier: " default ",
		ReasoningEffort:     " xhigh ",
		Endpoint:            " /v1/responses ",
		ExecutorType:        " openai ",
		InputTokens:         10,
		OutputTokens:        20,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
	}
	subject := repository.UsageEventCostSubject(event)
	if !subject.Timestamp.Equal(event.Timestamp) {
		t.Fatalf("event pricing subject lost CPA timestamp: got %s want %s", subject.Timestamp, event.Timestamp)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	})
	assertPricingSubject(t, subject)
	result := resolver.CalculateFee(subject)
	if !result.Available || math.Abs(result.TotalCostUSD-0.000018) > 1e-12 {
		t.Errorf("event subject pricing mismatch: %+v", result)
	}
}

func assertPricingSubject(t *testing.T, subject pricing.CostSubject) {
	t.Helper()
	wants := map[pricing.RuleField]string{
		pricing.RuleFieldAPIGroupKey:         "group-a",
		pricing.RuleFieldModel:               "model-a",
		pricing.RuleFieldAuthIndex:           "auth-a",
		pricing.RuleFieldModelAlias:          "alias-a",
		pricing.RuleFieldServiceTier:         "priority",
		pricing.RuleFieldResponseServiceTier: "default",
		pricing.RuleFieldReasoningEffort:     "xhigh",
		pricing.RuleFieldEndpoint:            "/v1/responses",
		pricing.RuleFieldExecutorType:        "openai",
	}
	for field, want := range wants {
		if got := subject.Dimensions.Value(field); got != want {
			t.Errorf("event subject field %s = %q, want %q", field, got, want)
		}
	}
	if subject.Tokens.InputTokens != 10 || subject.Tokens.OutputTokens != 20 || subject.Tokens.CacheReadTokens != 3 || subject.Tokens.CacheCreationTokens != 4 {
		t.Errorf("event subject token mapping mismatch: %+v", subject.Tokens)
	}
}
