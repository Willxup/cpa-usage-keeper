package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildAnalysisLatencyDiagnosticsHandlesBoundarySamples(t *testing.T) {
	t.Run("no valid samples", func(t *testing.T) {
		generate := true
		prewarm := false
		zero := int64(0)
		negative := int64(-1)
		validTTFT := int64(100)
		diagnostics := buildAnalysisLatencyDiagnosticsForTest(t, []entities.UsageEvent{
			{EventKey: "missing-ttft", Generate: &generate, LatencyMS: 1000},
			{EventKey: "zero-ttft", Generate: &generate, LatencyMS: 1000, TTFTMS: &zero},
			{EventKey: "negative-ttft", Generate: &generate, LatencyMS: 1000, TTFTMS: &negative},
			{EventKey: "zero-latency", Generate: &generate, LatencyMS: 0, TTFTMS: &validTTFT},
			{EventKey: "failed", Generate: &generate, Failed: true, LatencyMS: 1000, TTFTMS: &validTTFT},
			{EventKey: "prewarm", Generate: &prewarm, LatencyMS: 1000, TTFTMS: &validTTFT},
		})

		if diagnostics.TotalPoints != 0 || diagnostics.Sampled || diagnostics.P95TTFTMS != 0 || diagnostics.P95LatencyMS != 0 || diagnostics.MaxTTFTMS != 0 || diagnostics.MaxLatencyMS != 0 {
			t.Fatalf("expected empty diagnostics for invalid samples, got %+v", diagnostics)
		}
		if len(diagnostics.Points) != 0 || len(diagnostics.Density) != 0 {
			t.Fatalf("expected empty point and density arrays, got %+v", diagnostics)
		}
	})

	t.Run("single sample", func(t *testing.T) {
		generate := true
		ttftMS := int64(42)
		diagnostics := buildAnalysisLatencyDiagnosticsForTest(t, []entities.UsageEvent{{
			EventKey: "single", Generate: &generate, LatencyMS: 420, TTFTMS: &ttftMS,
		}})

		if diagnostics.TotalPoints != 1 || diagnostics.Sampled || diagnostics.P95TTFTMS != 42 || diagnostics.P95LatencyMS != 420 || diagnostics.MaxTTFTMS != 42 || diagnostics.MaxLatencyMS != 420 {
			t.Fatalf("expected single sample to define p95 and max, got %+v", diagnostics)
		}
		if len(diagnostics.Points) != 1 || diagnostics.Points[0].TTFTMS != 42 || diagnostics.Points[0].LatencyMS != 420 {
			t.Fatalf("expected the single pair to remain unchanged, got %+v", diagnostics.Points)
		}
	})

	t.Run("many equal values", func(t *testing.T) {
		generate := true
		maxTTFT := int64(900)
		equalTTFT := int64(400)
		minTTFT := int64(100)
		events := make([]entities.UsageEvent, 0, 259)
		events = append(events, entities.UsageEvent{EventKey: "max", Generate: &generate, LatencyMS: 9000, TTFTMS: &maxTTFT})
		for index := 0; index < 257; index++ {
			events = append(events, entities.UsageEvent{
				EventKey: fmt.Sprintf("equal-%03d", index), Generate: &generate, LatencyMS: 4000, TTFTMS: &equalTTFT,
			})
		}
		events = append(events, entities.UsageEvent{EventKey: "min", Generate: &generate, LatencyMS: 1000, TTFTMS: &minTTFT})

		diagnostics := buildAnalysisLatencyDiagnosticsForTest(t, events)
		if diagnostics.TotalPoints != 259 || diagnostics.Sampled || diagnostics.P95TTFTMS != 400 || diagnostics.P95LatencyMS != 4000 {
			t.Fatalf("expected exact nearest-rank p95 through the equal-value partition, got %+v", diagnostics)
		}
		if diagnostics.MaxTTFTMS != 900 || diagnostics.MaxLatencyMS != 9000 {
			t.Fatalf("expected max to remain based on the full scan, got %+v", diagnostics)
		}
		if len(diagnostics.Points) != 259 {
			t.Fatalf("expected all 259 unsampled points, got %d", len(diagnostics.Points))
		}
		if diagnostics.Points[0].TTFTMS != 900 || diagnostics.Points[0].LatencyMS != 9000 || diagnostics.Points[258].TTFTMS != 100 || diagnostics.Points[258].LatencyMS != 1000 {
			t.Fatalf("expected point order and pairs to precede in-place selection, got first=%+v last=%+v", diagnostics.Points[0], diagnostics.Points[len(diagnostics.Points)-1])
		}
	})
}

func TestBuildAnalysisLatencyDiagnosticsPreservesFilteredExactResults(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	generate := true
	prewarm := false

	ttft900 := int64(900)
	ttft120 := int64(120)
	ttft450 := int64(450)
	ttft300 := int64(300)
	ttftZero := int64(0)
	ttftNegative := int64(-1)
	ttftInvalid := int64(2500)
	events := []entities.UsageEvent{
		{EventKey: "valid-unordered-max", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(1 * time.Minute), LatencyMS: 5000, TTFTMS: &ttft900},
		{EventKey: "valid-low", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(2 * time.Minute), LatencyMS: 1000, TTFTMS: &ttft120},
		{EventKey: "valid-duplicate-a", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(3 * time.Minute), LatencyMS: 2300, TTFTMS: &ttft450},
		{EventKey: "valid-duplicate-b", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(4 * time.Minute), LatencyMS: 2300, TTFTMS: &ttft450},
		{EventKey: "valid-middle", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(5 * time.Minute), LatencyMS: 1600, TTFTMS: &ttft300},
		{EventKey: "invalid-null-ttft", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(6 * time.Minute), LatencyMS: 6000},
		{EventKey: "invalid-zero-ttft", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(7 * time.Minute), LatencyMS: 6000, TTFTMS: &ttftZero},
		{EventKey: "invalid-negative-ttft", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(8 * time.Minute), LatencyMS: 6000, TTFTMS: &ttftNegative},
		{EventKey: "invalid-zero-latency", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(9 * time.Minute), LatencyMS: 0, TTFTMS: &ttftInvalid},
		{EventKey: "invalid-negative-latency", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(10 * time.Minute), LatencyMS: -1, TTFTMS: &ttftInvalid},
		{EventKey: "invalid-failed", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(11 * time.Minute), Failed: true, LatencyMS: 9000, TTFTMS: &ttftInvalid},
		{EventKey: "invalid-prewarm", APIGroupKey: "sk-target", Generate: &prewarm, Timestamp: start.Add(12 * time.Minute), LatencyMS: 9000, TTFTMS: &ttftInvalid},
		{EventKey: "invalid-outside", APIGroupKey: "sk-target", Generate: &generate, Timestamp: start.Add(-time.Second), LatencyMS: 9000, TTFTMS: &ttftInvalid},
		{EventKey: "invalid-other-key", APIGroupKey: "sk-other", Generate: &generate, Timestamp: start.Add(13 * time.Minute), LatencyMS: 9000, TTFTMS: &ttftInvalid},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, APIGroupKey: "sk-target",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	if diagnostics.TotalPoints != 5 || diagnostics.Sampled {
		t.Fatalf("expected five unsampled valid pairs, got %+v", diagnostics)
	}
	if diagnostics.P95TTFTMS != 900 || diagnostics.P95LatencyMS != 5000 {
		t.Fatalf("expected exact nearest-rank p95 from all valid pairs, got %+v", diagnostics)
	}
	if diagnostics.MaxTTFTMS != 900 || diagnostics.MaxLatencyMS != 5000 {
		t.Fatalf("expected max values from all valid pairs, got %+v", diagnostics)
	}
	wantPoints := [][2]int64{{900, 5000}, {120, 1000}, {450, 2300}, {450, 2300}, {300, 1600}}
	if len(diagnostics.Points) != len(wantPoints) {
		t.Fatalf("expected %d points, got %+v", len(wantPoints), diagnostics.Points)
	}
	for index, want := range wantPoints {
		got := diagnostics.Points[index]
		if got.TTFTMS != want[0] || got.LatencyMS != want[1] {
			t.Fatalf("point %d = %+v, want ttft=%d latency=%d", index, got, want[0], want[1])
		}
	}
}

func TestBuildAnalysisLatencyDiagnosticsPreservesSampleOrderAndPairs(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	// 把样本放在查询窗口内部，使本测试只验证抽样顺序和配对，不依赖时间边界表示。
	sampleStart := start.Add(time.Second)
	generate := true
	const count = 2601
	events := make([]entities.UsageEvent, 0, count)
	for sourceIndex := 0; sourceIndex < count; sourceIndex++ {
		ttftMS := int64(count - sourceIndex)
		latencyMS := ttftMS*10000 + int64(sourceIndex)
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("latency-%04d", sourceIndex), APIGroupKey: "sk-target", Generate: &generate,
			Timestamp: sampleStart.Add(time.Duration(sourceIndex) * time.Millisecond), LatencyMS: latencyMS, TTFTMS: &ttftMS,
		})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, APIGroupKey: "sk-target",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	if diagnostics.TotalPoints != count || !diagnostics.Sampled || len(diagnostics.Points) != 2500 {
		t.Fatalf("expected %d total samples and 2500 deterministic points, got total=%d sampled=%v points=%d", count, diagnostics.TotalPoints, diagnostics.Sampled, len(diagnostics.Points))
	}
	if diagnostics.P95TTFTMS != 2471 || diagnostics.P95LatencyMS != 24710130 {
		t.Fatalf("expected exact nearest-rank p95 from all samples, got ttft=%d latency=%d", diagnostics.P95TTFTMS, diagnostics.P95LatencyMS)
	}
	if diagnostics.MaxTTFTMS != count || diagnostics.MaxLatencyMS != 26010000 {
		t.Fatalf("expected max values from all samples, got ttft=%d latency=%d", diagnostics.MaxTTFTMS, diagnostics.MaxLatencyMS)
	}

	// 固定几个等距抽样位置，确保选择算法不会先打乱原始顺序或拆散 TTFT/Latency 配对。
	wantSourceIndexes := map[int]int{0: 0, 1: 1, 1249: 1299, 1250: 1300, 2498: 2598, 2499: 2600}
	for pointIndex, sourceIndex := range wantSourceIndexes {
		wantTTFT := int64(count - sourceIndex)
		wantLatency := wantTTFT*10000 + int64(sourceIndex)
		got := diagnostics.Points[pointIndex]
		if got.TTFTMS != wantTTFT || got.LatencyMS != wantLatency {
			t.Fatalf("sample point %d = %+v, want source %d pair ttft=%d latency=%d", pointIndex, got, sourceIndex, wantTTFT, wantLatency)
		}
	}
}

func TestBuildAnalysisLatencyDiagnosticsAvoidsQuadraticAdversarialOrder(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	// 把样本放在查询窗口内部，使本测试只验证选择算法的退化保护。
	sampleStart := start.Add(time.Second)
	generate := true
	const count = 200000
	events := make([]entities.UsageEvent, 0, count)
	for sourceIndex := 0; sourceIndex < count; sourceIndex++ {
		value := int64(sourceIndex*2 + 1)
		if sourceIndex >= count/2 {
			value = int64((sourceIndex - count/2 + 1) * 2)
		}
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("adversarial-%06d", sourceIndex), APIGroupKey: "sk-target", Generate: &generate,
			Timestamp: sampleStart.Add(time.Duration(sourceIndex) * time.Microsecond), LatencyMS: value, TTFTMS: &value,
		})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, APIGroupKey: "sk-target",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	if diagnostics.P95TTFTMS != 190000 || diagnostics.P95LatencyMS != 190000 {
		t.Fatalf("expected exact p95 190000, got %+v", diagnostics)
	}
	if !diagnostics.Sampled || len(diagnostics.Points) != 2500 {
		t.Fatalf("expected 2500 sampled adversarial points, got %+v", diagnostics)
	}
	wantPoints := map[int]int64{0: 1, 1249: 199919, 1250: 80, 2499: 200000}
	for pointIndex, want := range wantPoints {
		got := diagnostics.Points[pointIndex]
		if got.TTFTMS != want || got.LatencyMS != want {
			t.Fatalf("point %d = %+v, want adversarial value %d", pointIndex, got, want)
		}
	}
}

func buildAnalysisLatencyDiagnosticsForTest(t *testing.T, events []entities.UsageEvent) repodto.AnalysisLatencyDiagnosticsRecord {
	t.Helper()
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	for index := range events {
		events[index].APIGroupKey = "sk-target"
		events[index].Timestamp = start.Add(time.Duration(index+1) * time.Second)
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{
		StartTime: &start, EndTime: &end, APIGroupKey: "sk-target",
	})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	return diagnostics
}
