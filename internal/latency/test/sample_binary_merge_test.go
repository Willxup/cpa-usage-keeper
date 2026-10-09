package test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func TestMergeBinaryRejectsMalformedFieldsAndAcceptsEmpty(t *testing.T) {
	makeBlob := func(points ...latency.SamplePoint) []byte {
		encoded := binary.AppendUvarint([]byte{latency.FormatVersion}, uint64(len(points)))
		for _, point := range points {
			encoded = binary.AppendUvarint(encoded, uint64(point.EventID))
			encoded = binary.AppendUvarint(encoded, point.Priority)
			encoded = binary.AppendUvarint(encoded, uint64(point.TTFTMS))
			encoded = binary.AppendUvarint(encoded, uint64(point.LatencyMS))
		}
		return encoded
	}
	source := latency.NewSampleSet()
	for id := int64(1); id <= 2; id++ {
		if err := source.Add(id, 10, 100); err != nil {
			t.Fatal(err)
		}
	}
	ordered := source.Points()
	badHash := ordered[0]
	badHash.Priority++
	zeroTTFT := ordered[0]
	zeroTTFT.TTFTMS = 0
	tooMany := binary.AppendUvarint([]byte{latency.FormatVersion}, latency.MaxEncodedSamplePoints+1)
	for name, encoded := range map[string][]byte{
		"priority mismatch":   makeBlob(badHash),
		"duplicate event":     makeBlob(ordered[0], ordered[0]),
		"out of order":        makeBlob(ordered[1], ordered[0]),
		"zero TTFT":           makeBlob(zeroTTFT),
		"count exceeds limit": tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := latency.NewQuerySampleMerger().MergeBinary(encoded); err == nil {
				t.Fatal("expected malformed sample rejection")
			}
			if _, err := latency.UnmarshalSampleSet(encoded); err == nil {
				t.Fatal("writer decoder accepted malformed sample")
			}
		})
	}
	empty := makeBlob()
	merger := latency.NewQuerySampleMerger()
	if count, err := merger.MergeBinary(empty); err != nil || count != 0 || merger.TakeSamples().Count() != 0 {
		t.Fatalf("empty blob count=%d err=%v", count, err)
	}
	var nilMerger *latency.QuerySampleMerger
	if _, err := nilMerger.MergeBinary(empty); err == nil {
		t.Fatal("nil receiver accepted BLOB")
	}
}

func TestMergeBinaryMatchesDecodeAndMerge(t *testing.T) {
	groups := make([]*latency.SampleSet, 8)
	for index := range groups {
		groups[index] = latency.NewSampleSet()
	}
	for id := int64(1); id <= 6800; id++ {
		group := groups[(id*7)%int64(len(groups))]
		if err := group.Add(id, 10+id%71, 100+id%131); err != nil {
			t.Fatal(err)
		}
	}
	if err := groups[6].Add(1, 11, 101); err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]int{{0, 1, 2, 3, 4, 5, 6, 7}, {7, 6, 5, 4, 3, 2, 1, 0}, {2, 5, 0, 7, 1, 6, 3, 4}} {
		want, merger := latency.NewSampleSet(), latency.NewQuerySampleMerger()
		for _, index := range order {
			blob, err := groups[index].MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := latency.UnmarshalSampleSet(blob)
			if err != nil {
				t.Fatal(err)
			}
			if err := want.Merge(decoded); err != nil {
				t.Fatal(err)
			}
			count, err := merger.MergeBinary(blob)
			if err != nil || count != groups[index].Count() {
				t.Fatalf("order %v row %d: count=%d err=%v", order, index, count, err)
			}
		}
		got := merger.TakeSamples()
		if !slices.Equal(got.Points(), want.Points()) {
			t.Fatalf("order %v changed ordered points", order)
		}
		gotBlob, _ := got.MarshalBinary()
		wantBlob, _ := want.MarshalBinary()
		if !bytes.Equal(gotBlob, wantBlob) {
			t.Fatalf("order %v changed BLOB", order)
		}
	}
}

func TestMergeBinaryValidatesNonWinningTailAndPreRowConflict(t *testing.T) {
	retained := latency.NewSampleSet()
	for id := int64(1); id <= 2500; id++ {
		if err := retained.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	worst := retained.Points()[latency.MaxSamplePoints-1]
	incoming := latency.NewSampleSet()
	for id := int64(2501); id <= 4999; id++ {
		if err := incoming.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	if err := incoming.Add(worst.EventID, worst.TTFTMS+1, worst.LatencyMS); err != nil {
		t.Fatal(err)
	}
	conflicting, err := incoming.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	retainedBlob, _ := retained.MarshalBinary()
	conflictMerger := latency.NewQuerySampleMerger()
	if _, err := conflictMerger.MergeBinary(retainedBlob); err != nil {
		t.Fatal(err)
	}
	if _, err := conflictMerger.MergeBinary(conflicting); err == nil {
		t.Fatal("conflict against pre-row retained point was skipped")
	}

	// 已满集合的截止点来自 5000 个事件；新行最后一个点明确高于截止点，不能入选。
	lowCutoff := latency.NewSampleSet()
	for id := int64(1); id <= 5000; id++ {
		if err := lowCutoff.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	highTail := latency.NewSampleSet()
	for id := int64(5001); id <= 7500; id++ {
		if err := highTail.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := lowCutoff.Points()[latency.MaxSamplePoints-1]
	tail := highTail.Points()[latency.MaxSamplePoints-1]
	if tail.Priority < cutoff.Priority || (tail.Priority == cutoff.Priority && tail.EventID <= cutoff.EventID) {
		t.Fatal("fixture tail must be unable to enter retained set")
	}
	valid, err := highTail.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for name, corrupt := range map[string][]byte{
		"nil":            nil,
		"trailing":       append(slices.Clone(valid), 0),
		"truncated tail": slices.Clone(valid[:len(valid)-1]),
		"bad version":    append([]byte{latency.FormatVersion + 1}, valid[1:]...),
	} {
		t.Run(name, func(t *testing.T) {
			baseBlob, _ := lowCutoff.MarshalBinary()
			merger := latency.NewQuerySampleMerger()
			if _, err := merger.MergeBinary(baseBlob); err != nil {
				t.Fatal(err)
			}
			if _, err := merger.MergeBinary(corrupt); err == nil {
				t.Fatal("expected corrupt BLOB rejection")
			}
			after, _ := merger.TakeSamples().MarshalBinary()
			if !bytes.Equal(baseBlob, after) {
				t.Fatal("failed merge changed retained set")
			}
		})
	}
}
