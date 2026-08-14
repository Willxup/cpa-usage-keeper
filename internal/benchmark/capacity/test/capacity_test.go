package capacity_test

import (
	"reflect"
	"slices"
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestCapacitySearchRampsFromLowestRateThenBisectsBoundary(t *testing.T) {
	search, err := capacity.NewCapacitySearch([]int{1, 2, 5, 10, 20, 30, 35, 40, 50, 75, 100})
	if err != nil {
		t.Fatalf("NewCapacitySearch returned error: %v", err)
	}
	var seen []int
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		seen = append(seen, rate)
		search.Record(rate, rate <= 32)
	}
	if want := []int{1, 2, 5, 10, 20, 40, 30, 35}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("probe order=%v, want %v", seen, want)
	}
	if search.HardCapacity() != 30 {
		t.Fatalf("hard capacity=%d, want highest passing configured rate 30", search.HardCapacity())
	}
	if search.FailureBoundary() != 35 {
		t.Fatalf("failure boundary=%d, want lowest failing configured rate 35", search.FailureBoundary())
	}
}

func TestCapacitySearchStopsWhenFirstRateFails(t *testing.T) {
	search, err := capacity.NewCapacitySearch([]int{1, 2, 5})
	if err != nil {
		t.Fatalf("NewCapacitySearch returned error: %v", err)
	}
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		search.Record(rate, false)
	}
	if search.HardCapacity() != 0 {
		t.Fatalf("hard capacity=%d", search.HardCapacity())
	}
}

func TestCapacitySearchStartsAtConfiguredRateAndFallsBackBelowIt(t *testing.T) {
	search, err := capacity.NewCapacitySearchAt([]int{1, 5, 10, 15, 20, 25, 50}, 25)
	if err != nil {
		t.Fatalf("NewCapacitySearchAt returned error: %v", err)
	}
	var seen []int
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		seen = append(seen, rate)
		search.Record(rate, rate <= 20)
	}
	if want := []int{25, 10, 15, 20}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("probe order=%v, want %v", seen, want)
	}
	if search.HardCapacity() != 20 || search.FailureBoundary() != 25 {
		t.Fatalf("capacity boundary=%d..%d, want 20..25", search.HardCapacity(), search.FailureBoundary())
	}
}

func TestCapacitySearchReturnsHighestRateWhenEveryProbePasses(t *testing.T) {
	search, err := capacity.NewCapacitySearch([]int{100, 200, 300, 400, 500, 750, 1000, 1500, 2000, 3000, 5000})
	if err != nil {
		t.Fatalf("NewCapacitySearch returned error: %v", err)
	}
	var seen []int
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		seen = append(seen, rate)
		search.Record(rate, true)
	}
	if want := []int{100, 200, 400, 1000, 2000, 5000}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("probe order=%v, want %v", seen, want)
	}
	if search.HardCapacity() != 5000 {
		t.Fatalf("hard capacity=%d, want 5000", search.HardCapacity())
	}
	if search.FailureBoundary() != 0 {
		t.Fatalf("failure boundary=%d, want no observed failure", search.FailureBoundary())
	}
}

func TestCapacitySearchCanPromoteAConfirmedBoundaryPass(t *testing.T) {
	search, err := capacity.NewCapacitySearch([]int{25, 50, 100, 200, 400})
	if err != nil {
		t.Fatalf("NewCapacitySearch returned error: %v", err)
	}
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		search.Record(rate, rate <= 100)
	}
	if search.HardCapacity() != 100 || search.FailureBoundary() != 200 {
		t.Fatalf("initial boundary=%d..%d, want 100..200", search.HardCapacity(), search.FailureBoundary())
	}
	if err := search.PromoteFailure(200); err != nil {
		t.Fatalf("PromoteFailure returned error: %v", err)
	}
	rate, ok := search.Next()
	if !ok || rate != 400 {
		t.Fatalf("next rate=%d ok=%v, want 400", rate, ok)
	}
	search.Record(rate, true)
	if search.HardCapacity() != 400 || search.FailureBoundary() != 0 {
		t.Fatalf("promoted boundary=%d..%d, want 400 with no failure", search.HardCapacity(), search.FailureBoundary())
	}
}

func TestEvaluateProbeSeparatesHardAndInteractiveCapacity(t *testing.T) {
	result := capacity.ProbeMetrics{
		OfferedEvents: 10_000, PublishedEvents: 10_000, DurableEvents: 9_995,
		BacklogStart: 0, BacklogEnd: 0, HTTPRequests: 1000,
		HTTPP95MS: 650, HTTPP99MS: 1800,
	}
	evaluation := capacity.EvaluateProbe(result, capacity.ProbeThresholds{MinDurableRatio: 0.99, InteractiveP95MS: 500, InteractiveP99MS: 2000})
	if !evaluation.HardPass {
		t.Fatalf("hard capacity should pass: %+v", evaluation)
	}
	if evaluation.InteractivePass {
		t.Fatalf("interactive capacity should fail p95: %+v", evaluation)
	}
}

func TestEvaluateProbeSeparatesCoreAndAnalysisLatencyErrors(t *testing.T) {
	diagnosticFailure := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 100,
		HTTPRequests: 10, AnalysisLatencyErrors: 1,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99, InteractiveP99MS: 3000})
	if !diagnosticFailure.HardPass || !diagnosticFailure.InteractivePass || diagnosticFailure.AnalysisLatencyPass {
		t.Fatalf("analysis latency errors must remain a separate diagnostic failure: %+v", diagnosticFailure)
	}

	coreFailure := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 100,
		HTTPRequests: 10, CoreHTTPErrors: 1,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99, InteractiveP99MS: 3000})
	if !coreFailure.HardPass || coreFailure.InteractivePass || !coreFailure.AnalysisLatencyPass {
		t.Fatalf("core HTTP errors must fail only the interactive gate: %+v", coreFailure)
	}

	ingestionFailure := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 50,
		AnalysisLatencyRequests: 1,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99, InteractiveP99MS: 3000})
	if ingestionFailure.HardPass || ingestionFailure.InteractivePass || !ingestionFailure.AnalysisLatencyPass {
		t.Fatalf("ingestion failure must not relabel a successful diagnostic request: %+v", ingestionFailure)
	}
}

func TestEvaluateProbeRejectsOOMErrorsAndGrowingBacklog(t *testing.T) {
	for _, metrics := range []capacity.ProbeMetrics{
		{OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 100, OOM: true},
		{OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 100, Errors: 1},
		{OfferedEvents: 100, PublishedEvents: 100, DurableEvents: 100, BacklogStart: 0, BacklogEnd: 10},
	} {
		evaluation := capacity.EvaluateProbe(metrics, capacity.ProbeThresholds{MinDurableRatio: 0.99, MaxBacklogGrowth: 0})
		if evaluation.HardPass {
			t.Fatalf("probe should fail: metrics=%+v evaluation=%+v", metrics, evaluation)
		}
	}
}

func TestEvaluateProbeRejectsDriverLag(t *testing.T) {
	evaluation := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 1000, PublishedEvents: 900, DurableEvents: 900,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99})
	if evaluation.HardPass {
		t.Fatalf("driver lag should fail: %+v", evaluation)
	}
	found := false
	for _, reason := range evaluation.Reasons {
		if reason == "driver_lag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("driver_lag reason missing: %+v", evaluation)
	}
}

func TestEvaluateProbeAllowsSubPermilleSchedulerTail(t *testing.T) {
	evaluation := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 15_000, PublishedEvents: 14_998, DurableEvents: 14_998,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99})
	if !evaluation.HardPass {
		t.Fatalf("sub-permille ticker tail must not become driver lag: %+v", evaluation)
	}
}

func TestEvaluateProbeAllowsOneLowRateDeliveryTail(t *testing.T) {
	evaluation := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 30, PublishedEvents: 30, DurableEvents: 29,
		BacklogStart: 0, BacklogEnd: 0, CheckpointLag: 0, IdentityPending: 0,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99})
	if !evaluation.HardPass {
		t.Fatalf("one fully drained low-rate delivery tail must not fail the cell: %+v", evaluation)
	}
}

func TestEvaluateProbeRejectsDrainBeyondStabilityBudget(t *testing.T) {
	evaluation := capacity.EvaluateProbe(capacity.ProbeMetrics{
		OfferedEvents: 30_000, PublishedEvents: 30_000, DurableEvents: 30_000,
		DrainSeconds: 5.1,
	}, capacity.ProbeThresholds{MinDurableRatio: 0.99, MaxDrainSeconds: 5})
	if evaluation.HardPass {
		t.Fatalf("probe that needs excessive catch-up must fail: %+v", evaluation)
	}
	if !slices.Contains(evaluation.Reasons, "drain_lag") {
		t.Fatalf("drain_lag reason missing: %+v", evaluation)
	}
}

func TestOOMProbeIsCapacityFailureNotCellInfrastructureFailure(t *testing.T) {
	oom := capacity.ProbeAttempt{Error: "Keeper exited before health check", Report: capacity.ProbeReport{Metrics: capacity.ProbeMetrics{OOM: true}}}
	if capacity.IsCapacityAttemptInfrastructureFailure(oom) {
		t.Fatal("OOM probe must remain a capacity boundary result")
	}
	for _, attempt := range []capacity.ProbeAttempt{
		{Error: "sampler failed"},
		{Report: capacity.ProbeReport{Metrics: capacity.ProbeMetrics{Panic: true}}},
	} {
		if !capacity.IsCapacityAttemptInfrastructureFailure(attempt) {
			t.Fatalf("non-OOM probe failure must remain terminal: %+v", attempt)
		}
	}
}

func TestSelectBoundaryCandidatesKeepsTopAndConservativeHalf(t *testing.T) {
	var attempts []capacity.ProbeAttempt
	for _, rate := range []int{1, 5, 20, 100, 200, 300, 350, 375} {
		attempts = append(attempts, capacity.ProbeAttempt{
			Phase: "search", RatePerSecond: rate,
			Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true}},
		})
	}
	if got := capacity.SelectBoundaryCandidates(attempts, 375, 2); !reflect.DeepEqual(got, []int{375, 100}) {
		t.Fatalf("boundary candidates=%v, want [375 100]", got)
	}
}

func TestSelectBoundaryCandidatesKeepsMinimumPassingFallback(t *testing.T) {
	var attempts []capacity.ProbeAttempt
	for _, rate := range []int{25, 50, 100, 200, 300, 350, 375} {
		attempts = append(attempts, capacity.ProbeAttempt{
			Phase: "search", RatePerSecond: rate,
			Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true}},
		})
	}
	if got := capacity.SelectBoundaryCandidates(attempts, 375, 3); !reflect.DeepEqual(got, []int{375, 100, 25}) {
		t.Fatalf("boundary candidates=%v, want [375 100 25]", got)
	}
}

func TestNormalizeFailureBoundaryKeepsOnlyStrictUpperBound(t *testing.T) {
	for _, test := range []struct {
		pass, failure, want int
	}{
		{pass: 100, failure: 125, want: 125},
		{pass: 100, failure: 100, want: 0},
		{pass: 100, failure: 75, want: 0},
		{pass: 100, failure: 0, want: 0},
	} {
		if got := capacity.NormalizeFailureBoundary(test.pass, test.failure); got != test.want {
			t.Fatalf("NormalizeFailureBoundary(%d, %d)=%d, want %d", test.pass, test.failure, got, test.want)
		}
	}
}
