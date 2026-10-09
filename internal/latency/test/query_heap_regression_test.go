package test

import (
	"encoding/binary"
	"reflect"
	"sort"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func queryPointsBlob(points []latency.SamplePoint) []byte {
	encoded := binary.AppendUvarint([]byte{latency.FormatVersion}, uint64(len(points)))
	for _, point := range points {
		encoded = binary.AppendUvarint(encoded, uint64(point.EventID))
		encoded = binary.AppendUvarint(encoded, point.Priority)
		encoded = binary.AppendUvarint(encoded, uint64(point.TTFTMS))
		encoded = binary.AppendUvarint(encoded, uint64(point.LatencyMS))
	}
	return encoded
}

func querySortPoints(points []latency.SamplePoint) {
	sort.Slice(points, func(i, j int) bool {
		if points[i].Priority != points[j].Priority {
			return points[i].Priority < points[j].Priority
		}
		return points[i].EventID < points[j].EventID
	})
}

func TestQueryMergerSustainedUpdatesMatchIndependentGlobalSort(t *testing.T) {
	all := make([]latency.SamplePoint, 0, 30000)
	rows := make([][]byte, 30)
	for bucket := range rows {
		points := make([]latency.SamplePoint, 1000)
		for index := range points {
			id := int64(bucket*1000 + index + 1)
			points[index] = latency.SamplePoint{EventID: id, Priority: legacyPriority(uint64(id)), TTFTMS: id + 10, LatencyMS: id + 100}
		}
		all = append(all, points...)
		querySortPoints(points)
		rows[bucket] = queryPointsBlob(points)
	}
	querySortPoints(all)
	for _, reverse := range []bool{false, true} {
		merger := latency.NewQuerySampleMerger()
		for index := range rows {
			selected := index
			if reverse {
				selected = len(rows) - index - 1
			}
			if count, err := merger.MergeBinary(rows[selected]); err != nil || count != 1000 {
				t.Fatalf("reverse=%v row=%d count=%d err=%v", reverse, selected, count, err)
			}
		}
		if got := merger.TakeSamples().Points(); !reflect.DeepEqual(got, all[:1000]) {
			t.Fatalf("reverse=%v query points differ from global sort", reverse)
		}
		if merger.TakeSamples() != nil {
			t.Fatal("sample merger transferred ownership twice")
		}
	}
}

func TestQueryMergerChecksOld2500RowTailConflictBeforeChangingState(t *testing.T) {
	first, ordered := legacySampleBlob(2500, false)
	target := ordered[999] // Equal to the retained worst point, so it must still be checked.
	second := make([]latency.SamplePoint, 0, 2500)
	below, above := 0, 0
	for id := int64(2501); id < 20000 && (below < 1200 || above < 1299); id++ {
		point := latency.SamplePoint{EventID: id, Priority: legacyPriority(uint64(id)), TTFTMS: id + 10, LatencyMS: id + 100}
		if point.Priority < target.Priority && below < 1200 {
			second = append(second, point)
			below++
		} else if point.Priority > target.Priority && above < 1299 {
			second = append(second, point)
			above++
		}
	}
	if below != 1200 || above != 1299 {
		t.Fatal("could not construct old-format tail conflict fixture")
	}
	target.TTFTMS++
	second = append(second, target)
	querySortPoints(second)
	if second[1200].EventID != target.EventID {
		t.Fatal("conflict must be after the new 1000-point cap in legacy row")
	}
	merger := latency.NewQuerySampleMerger()
	if _, err := merger.MergeBinary(first); err != nil {
		t.Fatal(err)
	}
	if _, err := merger.MergeBinary(queryPointsBlob(second)); err == nil {
		t.Fatal("equal-to-worst legacy tail conflict was skipped")
	}
	if got := merger.TakeSamples().Points(); !reflect.DeepEqual(got, ordered[:1000]) {
		t.Fatal("failed row changed the prior retained set")
	}
}

// A bounded merge validates conflicts against retained IDs, not all historical IDs.
// Compare with ordinary SampleSet.Merge at the same cap so this boundary cannot
// accidentally be attributed to the encoded-query cutoff optimization.
func TestQueryMergerConflictBoundaryMatchesBoundedMerge(t *testing.T) {
	blob, ordered := legacySampleBlob(2500, false)
	for _, tc := range []struct {
		name         string
		index        int
		wantConflict bool
	}{
		{"retained_best", 0, true},
		{"retained_cutoff", latency.MaxSamplePoints - 1, true},
		{"first_discarded", latency.MaxSamplePoints, false},
		{"legacy_tail", 2499, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := ordered[tc.index]
			changed.TTFTMS++
			nextBlob := queryPointsBlob([]latency.SamplePoint{changed})
			first, err := latency.UnmarshalSampleSet(blob)
			if err != nil {
				t.Fatal(err)
			}
			next, err := latency.UnmarshalSampleSet(nextBlob)
			if err != nil {
				t.Fatal(err)
			}
			// Before trimming, the full legacy row still detects this conflict.
			if err := first.Clone().Merge(next); err == nil {
				t.Fatal("full legacy row failed to detect conflicting event")
			}
			ordinary := latency.NewSampleSet()
			if err := ordinary.Merge(first); err != nil {
				t.Fatal(err)
			}
			query := latency.NewQuerySampleMerger()
			if _, err := query.MergeBinary(blob); err != nil {
				t.Fatal(err)
			}
			ordinaryErr := ordinary.Merge(next)
			_, queryErr := query.MergeBinary(nextBlob)
			if (ordinaryErr != nil) != tc.wantConflict || (queryErr != nil) != tc.wantConflict {
				t.Fatalf("want conflict=%v; ordinary=%v query=%v", tc.wantConflict, ordinaryErr, queryErr)
			}
			got := query.TakeSamples().Points()
			if !reflect.DeepEqual(got, ordinary.Points()) || !reflect.DeepEqual(got, ordered[:latency.MaxSamplePoints]) {
				t.Fatal("conflicting retained or discarded event changed selected points")
			}
		})
	}
}
