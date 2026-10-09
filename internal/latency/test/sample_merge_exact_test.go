package test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

// legacySampleMerge 在测试中独立保留旧的全量排序规则，精确比较集合和持久化字节。
func legacySampleMerge(t *testing.T, groups ...*latency.SampleSet) []latency.SamplePoint {
	t.Helper()
	byID := make(map[int64]latency.SamplePoint)
	for _, group := range groups {
		for _, point := range group.Points() {
			if previous, ok := byID[point.EventID]; ok && previous != point {
				t.Fatalf("conflicting oracle input %d", point.EventID)
			}
			byID[point.EventID] = point
		}
	}
	points := make([]latency.SamplePoint, 0, len(byID))
	for _, point := range byID {
		points = append(points, point)
	}
	slices.SortFunc(points, func(a, b latency.SamplePoint) int {
		if a.Priority < b.Priority {
			return -1
		}
		if a.Priority > b.Priority {
			return 1
		}
		if a.EventID < b.EventID {
			return -1
		}
		if a.EventID > b.EventID {
			return 1
		}
		return 0
	})
	return points[:min(len(points), latency.MaxSamplePoints)]
}

func TestSampleMergeMatchesLegacyExactPointsAndBytes(t *testing.T) {
	groups := make([]*latency.SampleSet, 4)
	for index := range groups {
		groups[index] = latency.NewSampleSet()
	}
	for eventID := int64(1); eventID <= 3900; eventID++ {
		group := groups[(eventID*7)%int64(len(groups))]
		if err := group.Add(eventID, 10+eventID%17, 100+eventID%29); err != nil {
			t.Fatal(err)
		}
	}
	// 同一真实事件跨行精确重复仍只出现一次。
	if err := groups[1].Add(1, 11, 101); err != nil {
		t.Fatal(err)
	}
	want := legacySampleMerge(t, groups...)
	sequential := latency.NewSampleSet()
	for eventID := int64(1); eventID <= 3900; eventID++ {
		if err := sequential.Add(eventID, 10+eventID%17, 100+eventID%29); err != nil {
			t.Fatal(err)
		}
	}
	if got := sequential.Points(); !slices.Equal(got, want) {
		t.Fatal("sequential Add changed ordered points")
	}
	if got := sequential.Clone().Points(); !slices.Equal(got, want) {
		t.Fatal("Clone changed ordered points")
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}} {
		merged := latency.NewSampleSet()
		for _, index := range order {
			if err := merged.Merge(groups[index]); err != nil {
				t.Fatal(err)
			}
		}
		if got := merged.Points(); !slices.Equal(got, want) {
			t.Fatalf("order %v changed ordered points", order)
		}
		if merged.Count() != latency.MaxSamplePoints {
			t.Fatalf("count=%d", merged.Count())
		}
		encoded, err := merged.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		legacyBytes := []byte{latency.FormatVersion}
		legacyBytes = binary.AppendUvarint(legacyBytes, uint64(len(want)))
		for _, point := range want {
			legacyBytes = binary.AppendUvarint(legacyBytes, uint64(point.EventID))
			legacyBytes = binary.AppendUvarint(legacyBytes, point.Priority)
			legacyBytes = binary.AppendUvarint(legacyBytes, uint64(point.TTFTMS))
			legacyBytes = binary.AppendUvarint(legacyBytes, uint64(point.LatencyMS))
		}
		if !bytes.Equal(encoded, legacyBytes) {
			t.Fatalf("order %v changed sample BLOB", order)
		}
	}
}

func TestSampleMergeChecksRetainedConflictBeforeEviction(t *testing.T) {
	left := latency.NewSampleSet()
	for id := int64(1); id <= 2500; id++ {
		if err := left.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	worst := left.Points()[latency.MaxSamplePoints-1]
	right := latency.NewSampleSet()
	for id := int64(2501); id <= 4999; id++ {
		if err := right.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	if err := right.Add(worst.EventID, worst.TTFTMS+1, worst.LatencyMS); err != nil {
		t.Fatal(err)
	}
	if right.Count() != latency.MaxSamplePoints {
		t.Fatal("conflicting point was not retained in incoming set")
	}
	if err := left.Merge(right); err == nil {
		t.Fatal("expected conflict against point retained before merge")
	}
}
