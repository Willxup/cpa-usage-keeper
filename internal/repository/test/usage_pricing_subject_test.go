package test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestPricingDimensionSubjectsMapAllNineFieldsAndFourTokenSegments(t *testing.T) {
	alias := " alias-a "
	event := entities.UsageEvent{
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
	hourly := entities.UsageOverviewHourlyStat{
		APIGroupKey:         event.APIGroupKey,
		Model:               event.Model,
		AuthIndex:           event.AuthIndex,
		ModelAlias:          alias,
		ServiceTier:         event.ServiceTier,
		ResponseServiceTier: event.ResponseServiceTier,
		ReasoningEffort:     event.ReasoningEffort,
		Endpoint:            event.Endpoint,
		ExecutorType:        event.ExecutorType,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
	}
	daily := entities.UsageOverviewDailyStat{
		APIGroupKey:         hourly.APIGroupKey,
		Model:               hourly.Model,
		AuthIndex:           hourly.AuthIndex,
		ModelAlias:          hourly.ModelAlias,
		ServiceTier:         hourly.ServiceTier,
		ResponseServiceTier: hourly.ResponseServiceTier,
		ReasoningEffort:     hourly.ReasoningEffort,
		Endpoint:            hourly.Endpoint,
		ExecutorType:        hourly.ExecutorType,
		InputTokens:         hourly.InputTokens,
		OutputTokens:        hourly.OutputTokens,
		CacheReadTokens:     hourly.CacheReadTokens,
		CacheCreationTokens: hourly.CacheCreationTokens,
	}
	record := repodto.UsageEventRecord{
		APIGroupKey:         event.APIGroupKey,
		Model:               event.Model,
		AuthIndex:           event.AuthIndex,
		ModelAlias:          alias,
		ServiceTier:         event.ServiceTier,
		ResponseServiceTier: event.ResponseServiceTier,
		ReasoningEffort:     event.ReasoningEffort,
		Endpoint:            event.Endpoint,
		ExecutorType:        event.ExecutorType,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
	}

	subjects := []pricing.CostSubject{
		repository.UsageEventCostSubject(event),
		repository.UsageEventRecordCostSubject(record),
		repository.UsageOverviewHourlyCostSubject(hourly),
		repository.UsageOverviewDailyCostSubject(daily),
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	})
	for index, subject := range subjects {
		assertPricingSubject(t, index, subject)
		result := resolver.Calculate(subject)
		if !result.Available || result.RuleMultiplier != 6 || math.Abs(result.Cost.TotalCostUSD-0.000018) > 1e-12 {
			t.Errorf("subject %d pricing mismatch: %+v", index, result)
		}
	}
}

func assertPricingSubject(t *testing.T, index int, subject pricing.CostSubject) {
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
			t.Errorf("subject %d field %s = %q, want %q", index, field, got, want)
		}
	}
	if subject.Tokens.InputTokens != 10 || subject.Tokens.OutputTokens != 20 || subject.Tokens.CacheReadTokens != 3 || subject.Tokens.CacheCreationTokens != 4 {
		t.Errorf("subject %d token mapping mismatch: %+v", index, subject.Tokens)
	}
}
