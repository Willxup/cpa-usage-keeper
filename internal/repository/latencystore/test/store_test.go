package test

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"
)

func TestQueryMergeMatchesDecodedSampleOracleExactly(t *testing.T) {
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 6400)
	for index := 0; index < 6400; index++ {
		id := int64(index + 1)
		events = append(events, latencyStoreTestEvent(id, start.Add(time.Duration(index/800)*time.Hour+time.Duration(index%800)*time.Second), 10+id%107, 100+id%251))
	}
	rows := latencyStoreTestRows(t, events, entities.UsageLatencyBucketHour)
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	for _, order := range [][]entities.UsageLatencyStat{rows, reversed} {
		wantSamples := latency.NewSampleSet()
		wantTTFT, wantLatency := latency.NewSketch(), latency.NewSketch()
		var wantCount, wantMaxTTFT, wantMaxLatency int64
		for _, row := range order {
			decoded, err := latency.UnmarshalSampleSet(row.SamplePoints)
			if err != nil {
				t.Fatal(err)
			}
			if err := wantSamples.Merge(decoded); err != nil {
				t.Fatal(err)
			}
			ttft, err := latency.UnmarshalSketch(row.TTFTSketch)
			if err != nil {
				t.Fatal(err)
			}
			latencySketch, err := latency.UnmarshalSketch(row.LatencySketch)
			if err != nil {
				t.Fatal(err)
			}
			if err := wantTTFT.Merge(ttft); err != nil {
				t.Fatal(err)
			}
			if err := wantLatency.Merge(latencySketch); err != nil {
				t.Fatal(err)
			}
			wantCount += row.SampleCount
			wantMaxTTFT = max(wantMaxTTFT, row.MaxTTFTMS)
			wantMaxLatency = max(wantMaxLatency, row.MaxLatencyMS)
		}
		got, err := latencystore.MergeDiagnosticsRows(order)
		if err != nil {
			t.Fatal(err)
		}
		wantBlob, _ := wantSamples.MarshalBinary()
		gotBlob, _ := got.SamplePoints.MarshalBinary()
		if !bytes.Equal(gotBlob, wantBlob) || got.SampleCount != wantCount || got.MaxTTFTMS != wantMaxTTFT || got.MaxLatencyMS != wantMaxLatency || got.TTFTSketch.P95() != wantTTFT.P95() || got.LatencySketch.P95() != wantLatency.P95() {
			t.Fatalf("query merge differs from decoded merge oracle: points=%d/%d count=%d/%d p95=%d,%d/%d,%d", got.SamplePoints.Count(), wantSamples.Count(), got.SampleCount, wantCount, got.TTFTSketch.P95(), got.LatencySketch.P95(), wantTTFT.P95(), wantLatency.P95())
		}
	}
}

func TestLatencyDiagnosticsMergeCombinesExactCountersSketchesAndStableSamples(t *testing.T) {
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 3000)
	for index := 0; index < 3000; index++ {
		value := int64(index + 1)
		events = append(events, latencyStoreTestEvent(value, start.Add(time.Duration(index)*2*time.Second), value, value*10))
	}
	rows := latencyStoreTestRows(t, events, entities.UsageLatencyBucketHour)
	if len(rows) < 2 {
		t.Fatalf("expected multiple hour buckets, got %d rows", len(rows))
	}

	aggregate, err := latencystore.MergeDiagnosticsRows(rows)
	if err != nil {
		t.Fatalf("MergeDiagnosticsRows returned error: %v", err)
	}
	if aggregate.SampleCount != 3000 || aggregate.MaxTTFTMS != 3000 || aggregate.MaxLatencyMS != 30000 {
		t.Fatalf("unexpected exact diagnostics counters: %+v", aggregate)
	}
	assertLatencySketchP95Close(t, aggregate.TTFTSketch.P95(), 2850)
	assertLatencySketchP95Close(t, aggregate.LatencySketch.P95(), 28500)
	points := aggregate.SamplePoints.Points()
	if len(points) != latency.MaxSamplePoints {
		t.Fatalf("expected %d stable points, got %d", latency.MaxSamplePoints, len(points))
	}
	for _, point := range points {
		if point.LatencyMS != point.TTFTMS*10 {
			t.Fatalf("sample pair was split during merge: %+v", point)
		}
	}

	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	reverseAggregate, err := latencystore.MergeDiagnosticsRows(reversed)
	if err != nil {
		t.Fatalf("MergeDiagnosticsRows returned reverse-order error: %v", err)
	}
	if !slices.Equal(points, reverseAggregate.SamplePoints.Points()) {
		t.Fatal("expected stable samples to be independent of row merge order")
	}
}

func TestLatencyDiagnosticsMergeRejectsCorruptRowsWithoutPartialAggregate(t *testing.T) {
	start := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	baseRows := latencyStoreTestRows(t, []entities.UsageEvent{
		latencyStoreTestEvent(1, start.Add(10*time.Minute), 100, 1000),
		latencyStoreTestEvent(2, start.Add(time.Hour+10*time.Minute), 200, 2000),
	}, entities.UsageLatencyBucketHour)
	if len(baseRows) != 2 {
		t.Fatalf("expected two base rows, got %d", len(baseRows))
	}

	testCases := []struct {
		name   string
		mutate func([]entities.UsageLatencyStat)
	}{
		{
			name: "format version",
			mutate: func(rows []entities.UsageLatencyStat) {
				rows[1].FormatVersion++
			},
		},
		{
			name: "TTFT sketch",
			mutate: func(rows []entities.UsageLatencyStat) {
				rows[1].TTFTSketch = []byte{latency.FormatVersion}
			},
		},
		{
			name: "latency sketch",
			mutate: func(rows []entities.UsageLatencyStat) {
				rows[1].LatencySketch = []byte{latency.FormatVersion}
			},
		},
		{
			name: "sample points",
			mutate: func(rows []entities.UsageLatencyStat) {
				rows[1].SamplePoints = []byte{latency.FormatVersion}
			},
		},
		{
			name: "sample count mismatch",
			mutate: func(rows []entities.UsageLatencyStat) {
				rows[1].SampleCount++
			},
		},
		{
			name: "int64 sample count overflow",
			mutate: func(rows []entities.UsageLatencyStat) {
				maxCountSketch := []byte{latency.FormatVersion, 1, 0}
				maxCountSketch = binary.AppendUvarint(maxCountSketch, math.MaxInt64)
				rows[0].SampleCount = math.MaxInt64
				rows[0].TTFTSketch = slices.Clone(maxCountSketch)
				rows[0].LatencySketch = slices.Clone(maxCountSketch)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows := slices.Clone(baseRows)
			testCase.mutate(rows)
			aggregate, err := latencystore.MergeDiagnosticsRows(rows)
			if err == nil {
				t.Fatalf("expected corrupt diagnostics row to fail, got %+v", aggregate)
			}
			if aggregate != (latencystore.DiagnosticsAggregate{}) {
				t.Fatalf("expected zero aggregate on error, got %+v", aggregate)
			}
		})
	}
}

func latencyStoreTestRows(t *testing.T, events []entities.UsageEvent, bucketType entities.UsageLatencyBucketType) []entities.UsageLatencyStat {
	t.Helper()
	now := events[len(events)-1].Timestamp
	rows, err := latency.BuildRows(events, now)
	if err != nil {
		t.Fatalf("BuildRows returned error: %v", err)
	}
	selected := make([]entities.UsageLatencyStat, 0, len(rows))
	for _, row := range rows {
		if row.BucketType == bucketType {
			selected = append(selected, row)
		}
	}
	return selected
}

func latencyStoreTestEvent(id int64, timestamp time.Time, ttftMS, latencyMS int64) entities.UsageEvent {
	generate := true
	return entities.UsageEvent{ID: id, Timestamp: timestamp, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: latencyMS}
}

func assertLatencySketchP95Close(t *testing.T, got, want int64) {
	t.Helper()
	relativeError := math.Abs(float64(got-want)) / float64(want)
	if relativeError > latency.RelativeAccuracy {
		t.Fatalf("P95=%d, want %d within %.2f%% relative error (got %.4f%%)", got, want, latency.RelativeAccuracy*100, relativeError*100)
	}
}
