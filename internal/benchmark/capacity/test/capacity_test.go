package capacity_test

import (
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestCapacitySearchUsesBoundedDiscreteBinarySearch(t *testing.T) {
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
	if len(seen) > 4 {
		t.Fatalf("probe count=%d, want at most 4: %v", len(seen), seen)
	}
	if search.HardCapacity() != 30 {
		t.Fatalf("hard capacity=%d, want highest passing configured rate 30", search.HardCapacity())
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
	if len(seen) > 4 {
		t.Fatalf("probe count=%d, want at most 4: %v", len(seen), seen)
	}
	if search.HardCapacity() != 5000 {
		t.Fatalf("hard capacity=%d, want 5000", search.HardCapacity())
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
