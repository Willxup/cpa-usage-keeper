package capacity_test

import (
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestBuildSummaryProducesOneRecommendationPerResource(t *testing.T) {
	results := []capacity.CellResult{
		cellResult("one", "1c-unlimited", 1, 150, 100, 304*1024*1024),
		cellResult("two", "2c-unlimited", 2, 200, 200, 417*1024*1024),
	}
	results[0].Capacity.LowestHardFailureEventsPerSecond = 175
	results[0].Capacity.LowestInteractiveFailureEventsPerSecond = 125
	summary := capacity.BuildSummary(results)
	if len(summary.Hardware) != 2 {
		t.Fatalf("hardware rows=%d, want 2", len(summary.Hardware))
	}
	if summary.Hardware[0].DatasetID != "reference-3m" || summary.Hardware[0].IngestionMaxEventsPerSecond != 150 || summary.Hardware[0].IngestionLowestFailureRate != 175 || summary.Hardware[0].DashboardMaxEventsPerSecond != 100 || summary.Hardware[0].DashboardLowestFailureRate != 125 {
		t.Fatalf("unexpected first recommendation: %+v", summary.Hardware[0])
	}
	if summary.Hardware[1].CPU != 2 || summary.Hardware[1].PeakMemoryBytes != 417*1024*1024 {
		t.Fatalf("unexpected second recommendation: %+v", summary.Hardware[1])
	}
}

func TestBuildSummaryUsesBoundaryLatencyMedian(t *testing.T) {
	result := cellResult("cell", "1c-unlimited", 1, 100, 70, 100)
	result.Capacity.InteractiveEventsPerSecond = 100
	result.Attempts = []capacity.ProbeAttempt{
		{Phase: "boundary-pass", RatePerSecond: 100, Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true, InteractivePass: true}, Metrics: capacity.ProbeMetrics{HTTPP95MS: 400, HTTPP99MS: 800}}},
		{Phase: "boundary-pass", RatePerSecond: 100, Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true, InteractivePass: true}, Metrics: capacity.ProbeMetrics{HTTPP95MS: 600, HTTPP99MS: 1200}}},
	}
	summary := capacity.BuildSummary([]capacity.CellResult{result})
	if summary.Cells[0].BoundaryHTTPP95MS != 500 || summary.Cells[0].BoundaryHTTPP99MS != 1000 {
		t.Fatalf("unexpected boundary latency: %+v", summary.Cells[0])
	}
}

func TestBuildSummaryUsesDashboardCapacityForDashboardLatency(t *testing.T) {
	result := cellResult("cell", "2c-unlimited", 2, 200, 70, 100)
	result.Capacity.InteractiveEventsPerSecond = 100
	result.Attempts = []capacity.ProbeAttempt{
		{Phase: "soak", RatePerSecond: 200, Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true}, Metrics: capacity.ProbeMetrics{HTTPP95MS: 900, HTTPP99MS: 3200}}, Resource: capacity.AttemptResource{CPUUtilizationPercent: 80}},
		{Phase: "soak", RatePerSecond: 100, Report: capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true, InteractivePass: true}, Metrics: capacity.ProbeMetrics{HTTPP95MS: 200, HTTPP99MS: 500}}},
	}
	summary := capacity.BuildSummary([]capacity.CellResult{result})
	cell := summary.Cells[0]
	if cell.BoundaryHTTPP95MS != 200 || cell.BoundaryHTTPP99MS != 500 {
		t.Fatalf("dashboard latency came from the wrong rate: %+v", cell)
	}
	if cell.CapacityCPUUtilizationPercent != 80 {
		t.Fatalf("ingestion telemetry must remain tied to the hard capacity: %+v", cell)
	}
}

func TestRecommendedEventsPerSecondAppliesManifestRatio(t *testing.T) {
	if got := capacity.RecommendedEventsPerSecond(100, 0.70); got != 70 {
		t.Fatalf("recommended rate=%d, want 70", got)
	}
}

func TestBuildSummaryExplainsDashboardAtFiveMinuteSearchCapacity(t *testing.T) {
	result := cellResult("cell", "2c-unlimited", 2, 200, 0, 400*1024*1024)
	result.Attempts = []capacity.ProbeAttempt{
		{
			Phase: "search", RatePerSecond: 200, DurationSeconds: 300,
			Report: capacity.ProbeReport{
				Evaluation:      capacity.ProbeEvaluation{HardPass: true, InteractivePass: false, AnalysisLatencyPass: true},
				Metrics:         capacity.ProbeMetrics{HTTPP95MS: 650, HTTPP99MS: 3100, AnalysisLatencyRequests: 9},
				AnalysisLatency: capacity.LatencySummary{Samples: 9, P50MS: 3300, P95MS: 3500, P99MS: 4000, MaxMS: 4000},
				LatencyByPath: map[string]capacity.LatencySummary{
					"/overview": {P95MS: 300, P99MS: 500},
					"/analysis": {P95MS: 900, P99MS: 1000},
					"/api/v1/usage/analysis/latency?range=30d": {P95MS: 3500, P99MS: 4000},
				},
			},
		},
	}
	summary := capacity.BuildSummary([]capacity.CellResult{result})
	cell := summary.Cells[0]
	if cell.BoundaryHTTPP95MS != 650 || cell.BoundaryHTTPP99MS != 3100 {
		t.Fatalf("search capacity latency not selected: %+v", cell)
	}
	if cell.DashboardStatus != "ingestion_only" || cell.SlowestDashboardPath != "/analysis" || cell.SlowestDashboardP99MS != 1000 {
		t.Fatalf("unexpected dashboard assessment: %+v", cell)
	}
	if cell.AnalysisLatencySamples != 9 || cell.AnalysisLatencyStatus != "passed" || cell.AnalysisLatencyP95MS != 3500 || cell.AnalysisLatencyP99MS != 4000 {
		t.Fatalf("analysis latency diagnostics were not reported separately: %+v", cell)
	}
}

func TestBuildSummaryExplainsIngestionOnlyCapacity(t *testing.T) {
	result := cellResult("cell", "4c-unlimited", 4, 1000, 0, 600*1024*1024)
	summary := capacity.BuildSummary([]capacity.CellResult{result})
	want := "可持续 ingestion 上限为 1000 events/s；Dashboard 未通过交互 SLA"
	if summary.Hardware[0].Guidance != want {
		t.Fatalf("guidance=%q, want %q", summary.Hardware[0].Guidance, want)
	}
}

func TestBuildSummarySeparatesDashboardMaximumFromRecommendedRate(t *testing.T) {
	result := cellResult("cell", "4c-unlimited", 4, 1000, 700, 220*1024*1024)
	result.Capacity.InteractiveEventsPerSecond = 900
	result.Attempts = []capacity.ProbeAttempt{
		{
			Phase: "soak", RatePerSecond: 1000,
			Report:       capacity.ProbeReport{Evaluation: capacity.ProbeEvaluation{HardPass: true, InteractivePass: false}},
			Resource:     capacity.AttemptResource{CPUUtilizationPercent: 79.5},
			PeakResource: capacity.CgroupSample{MemoryPeakBytes: 210 * 1024 * 1024},
		},
	}
	summary := capacity.BuildSummary([]capacity.CellResult{result})
	cell := summary.Cells[0]
	if cell.CapacityCPUUtilizationPercent != 79.5 || cell.CapacityPeakMemoryBytes != 210*1024*1024 {
		t.Fatalf("capacity telemetry missing: %+v", cell)
	}
	recommendation := summary.Hardware[0]
	if recommendation.DashboardMaxEventsPerSecond != 900 || recommendation.RecommendedEventsPerSecond != 700 {
		t.Fatalf("dashboard maximum and recommendation were conflated: %+v", recommendation)
	}
}

func cellResult(id, resourceID string, cpu float64, hard, recommended int, peak int64) capacity.CellResult {
	return capacity.CellResult{
		Cell: capacity.Cell{
			ID: id, DatasetID: "reference-3m", Cardinality: capacity.Cardinality{Identities: 500, Models: 50, APIKeys: 50},
			Resource: capacity.Resource{ID: resourceID, CPU: cpu, MemoryMiB: 0},
		},
		Status: "completed", StartupSeconds: 1,
		Capacity:     capacity.CapacityResult{HardEventsPerSecond: hard, InteractiveEventsPerSecond: recommended, RecommendedEventsPerSecond: recommended},
		PeakResource: capacity.CgroupSample{MemoryPeakBytes: peak},
	}
}
