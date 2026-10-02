package test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }

func completePricing(model string) pricing.ModelPricingConfig {
	return pricing.ModelPricingConfig{
		Model: model, PricingStyle: "openai", BasePrices: pricing.BasePrices{Input: 1}, ModelMultiplier: 1,
		ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{},
	}
}

func branch(id string, context pricing.ContextCondition, period pricing.PeriodCondition, inputPrice float64) pricing.PriceBranch {
	return pricing.PriceBranch{ID: id, Name: id, Context: context, Period: period, Prices: pricing.BasePrices{Input: inputPrice}}
}

func mustPricingSnapshot(t *testing.T, location string, configs ...pricing.ModelPricingConfig) *pricing.Snapshot {
	t.Helper()
	zone, err := time.LoadLocation(location)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := pricing.CompilePricingSnapshot(configs, zone)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func mustTimestamp(t *testing.T, value string) time.Time {
	t.Helper()
	result, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertFee(t *testing.T, got pricing.FeeResult, wantCost float64, wantAvailable bool) {
	t.Helper()
	if math.IsNaN(got.TotalCostUSD) || math.IsInf(got.TotalCostUSD, 0) || math.Abs(got.TotalCostUSD-wantCost) > 1e-12 || got.Available != wantAvailable {
		t.Fatalf("got %+v, want %f available=%v", got, wantCost, wantAvailable)
	}
}

func TestCompleteResolverCalculatesFixedExamples(t *testing.T) {
	for _, example := range loadPricingExamples(t) {
		t.Run(example.Name, func(t *testing.T) {
			configs := []pricing.ModelPricingConfig{}
			if example.Config != nil {
				configs = append(configs, *example.Config)
			}
			snapshot := mustPricingSnapshot(t, "Asia/Shanghai", configs...)
			subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: example.Input.Model, ServiceTier: example.Input.ServiceTier}, helper.UsageTokenCostInput{
				InputTokens: example.Input.InputTokens, OutputTokens: example.Input.OutputTokens,
				CacheReadTokens: example.Input.CacheReadTokens, CacheCreationTokens: example.Input.CacheCreationTokens,
			})
			subject.Timestamp = mustTimestamp(t, example.Input.Timestamp)
			assertFee(t, pricing.NewCatalog(snapshot).NewResolver().CalculateFee(subject), example.Want.TotalCostUSD, example.Want.Available)
		})
	}
}

func TestCompleteResolverMatchesContextIntervalsAndEntireRequest(t *testing.T) {
	config := completePricing("model")
	config.Branches = []pricing.PriceBranch{
		branch("lte", pricing.ContextCondition{Type: pricing.ContextLTE, Threshold: int64Pointer(10)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2),
		branch("range", pricing.ContextCondition{Type: pricing.ContextRange, Min: int64Pointer(11), Max: int64Pointer(20)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 3),
		branch("gt", pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(20)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 4),
	}
	resolver := pricing.NewCatalog(mustPricingSnapshot(t, "UTC", config)).NewResolver()
	for _, tc := range []struct {
		tokens int64
		want   float64
	}{{0, 0}, {10, 0.00002}, {11, 0.000033}, {20, 0.00006}, {21, 0.000084}} {
		subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{InputTokens: tc.tokens})
		subject.Timestamp = mustTimestamp(t, "2026-09-23T09:00:00Z")
		assertFee(t, resolver.CalculateFee(subject), tc.want, true)
	}
}

func TestCompleteResolverUsesDeploymentClockAcrossMidnightAndDST(t *testing.T) {
	config := completePricing("model")
	config.Branches = []pricing.PriceBranch{branch("night", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("20:00"), End: stringPointer("08:00")}, 2)}
	resolver := pricing.NewCatalog(mustPricingSnapshot(t, "Asia/Shanghai", config)).NewResolver()
	for _, tc := range []struct {
		timestamp string
		want      float64
	}{
		{"2026-09-23T19:59:00+08:00", 1}, {"2026-09-23T20:00:00+08:00", 2}, {"2026-09-23T12:00:00Z", 2},
		{"2026-09-24T07:59:00+08:00", 2}, {"2026-09-24T08:00:00+08:00", 1},
	} {
		subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
		subject.Timestamp = mustTimestamp(t, tc.timestamp)
		assertFee(t, resolver.CalculateFee(subject), tc.want, true)
	}
	config.Branches[0].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("01:00"), End: stringPointer("02:00")}
	resolver = pricing.NewCatalog(mustPricingSnapshot(t, "America/Los_Angeles", config)).NewResolver()
	for _, timestamp := range []string{"2026-11-01T01:30:00-07:00", "2026-11-01T01:30:00-08:00"} {
		subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
		subject.Timestamp = mustTimestamp(t, timestamp)
		assertFee(t, resolver.CalculateFee(subject), 2, true)
	}
}

func TestCompleteResolverMatchesWeekdaysInDeploymentTimezone(t *testing.T) {
	config := completePricing("model")
	weekday := branch("weekday", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2)
	weekday.Days = pricing.DaysWeekday
	weekend := branch("weekend", pricing.ContextCondition{Type: pricing.ContextAll}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 3)
	weekend.Days = pricing.DaysWeekend
	config.Branches = []pricing.PriceBranch{weekday, weekend}
	resolver := pricing.NewCatalog(mustPricingSnapshot(t, "Asia/Shanghai", config)).NewResolver()
	for _, tc := range []struct {
		timestamp string
		want      float64
	}{
		{"2026-09-25T15:59:00Z", 2}, // 周五 23:59
		{"2026-09-25T16:00:00Z", 3}, // 周六 00:00
		{"2026-09-27T15:59:00Z", 3}, // 周日 23:59
		{"2026-09-27T16:00:00Z", 2}, // 周一 00:00
	} {
		subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
		subject.Timestamp = mustTimestamp(t, tc.timestamp)
		assertFee(t, resolver.CalculateFee(subject), tc.want, true)
	}
}

func TestCompleteResolverRejectsOnlyJointlyOverlappingBranches(t *testing.T) {
	config := completePricing("model")
	config.Branches = []pricing.PriceBranch{
		branch("a", pricing.ContextCondition{Type: pricing.ContextRange, Min: int64Pointer(10), Max: int64Pointer(20)}, pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("22:00"), End: stringPointer("02:00")}, 2),
		branch("b", pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(19)}, pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("01:00"), End: stringPointer("03:00")}, 3),
	}
	_, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC)
	var conflict *pricing.BranchConflictError
	if !errors.As(err, &conflict) || conflict.BranchIDs != [2]string{"a", "b"} || len(conflict.FieldPaths) != 6 {
		t.Fatalf("expected branch conflict with both IDs and field paths, got %v", err)
	}
	config.Branches[1].Period = pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: stringPointer("02:00"), End: stringPointer("03:00")}
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err != nil {
		t.Fatalf("adjacent half-open windows must not conflict: %v", err)
	}
	config.Branches[1].Period = pricing.PeriodCondition{Type: pricing.PeriodAll}
	config.Branches[1].Context = pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(20)}
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.UTC); err != nil {
		t.Fatalf("disjoint context intervals must not conflict: %v", err)
	}
}

func TestCompletePricingSnapshotOwnsDeepCopy(t *testing.T) {
	config := completePricing("model")
	config.ConditionalMultipliers = []pricing.RuleConfig{{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2}}
	config.Branches = []pricing.PriceBranch{branch("a", pricing.ContextCondition{Type: pricing.ContextGT, Threshold: int64Pointer(10)}, pricing.PeriodCondition{Type: pricing.PeriodAll}, 2)}
	snapshot := mustPricingSnapshot(t, "UTC", config)
	config.Branches[0].Prices.Input = 100
	*config.Branches[0].Context.Threshold = 100
	config.ConditionalMultipliers[0].Multiplier = 100
	first, ok := snapshot.PricingModelConfig("model")
	if !ok || first.Branches[0].Prices.Input != 2 || *first.Branches[0].Context.Threshold != 10 || first.ConditionalMultipliers[0].Multiplier != 2 {
		t.Fatalf("snapshot retained mutable input: %+v", first)
	}
	first.Branches[0].Prices.Input = 200
	*first.Branches[0].Context.Threshold = 200
	second, _ := snapshot.PricingModelConfig("model")
	if second.Branches[0].Prices.Input != 2 || *second.Branches[0].Context.Threshold != 10 {
		t.Fatalf("snapshot leaked mutable output: %+v", second)
	}
	empty := completePricing("empty-rules")
	emptySnapshot := mustPricingSnapshot(t, "UTC", empty)
	encoded, err := json.Marshal(emptySnapshot.PricingModelConfigs()[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded pricing.ModelPricingConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("complete config must round-trip through JSON: %v: %s", err, encoded)
	}
	if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{decoded}, time.UTC); err != nil {
		t.Fatalf("round-tripped config must compile: %v", err)
	}
}
