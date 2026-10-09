package test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func querySketchBlob(bins ...struct {
	key   int64
	count uint64
}) []byte {
	encoded := binary.AppendUvarint([]byte{latency.FormatVersion}, uint64(len(bins)))
	for _, bin := range bins {
		encoded = binary.AppendUvarint(encoded, uint64(bin.key<<1)^uint64(bin.key>>63))
		encoded = binary.AppendUvarint(encoded, bin.count)
	}
	return encoded
}

func TestQuerySketchMergerMatchesDecodedMergeExactly(t *testing.T) {
	want := latency.NewSketch()
	query := latency.NewQuerySketchMerger()
	for row := 0; row < 50; row++ {
		part := latency.NewSketch()
		for value := 1; value <= 80; value++ {
			if err := part.Add(int64(1 + (row*value*37)%100000)); err != nil {
				t.Fatal(err)
			}
		}
		encoded, err := part.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := latency.UnmarshalSketch(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := want.Merge(decoded); err != nil {
			t.Fatal(err)
		}
		if count, err := query.MergeBinary(encoded); err != nil || count != part.Count() {
			t.Fatalf("row %d count=%d err=%v", row, count, err)
		}
	}
	got := query.TakeSketch()
	if got == nil || got.Count() != want.Count() || got.P95() != want.P95() {
		t.Fatal("query sketch count or P95 differs from decoded merge")
	}
	gotBytes, _ := got.MarshalBinary()
	wantBytes, _ := want.MarshalBinary()
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatal("query sketch bins differ from decoded merge")
	}
	if query.TakeSketch() != nil {
		t.Fatal("query sketch merger transferred ownership twice")
	}
	if _, err := query.MergeBinary(wantBytes); err == nil {
		t.Fatal("query sketch merger accepted input after TakeSketch")
	}
}

func TestQuerySketchMergerSharesStrictDecoderAndClosesOnFailure(t *testing.T) {
	good := querySketchBlob(struct {
		key   int64
		count uint64
	}{0, 1})
	badCases := map[string][]byte{
		"empty":     nil,
		"version":   {latency.FormatVersion + 1, 0},
		"truncated": {latency.FormatVersion, 1, 0},
		"zero bin": querySketchBlob(struct {
			key   int64
			count uint64
		}{0, 0}),
		"duplicate": querySketchBlob(struct {
			key   int64
			count uint64
		}{0, 1}, struct {
			key   int64
			count uint64
		}{0, 1}),
		"out of order": querySketchBlob(struct {
			key   int64
			count uint64
		}{1, 1}, struct {
			key   int64
			count uint64
		}{0, 1}),
		"trailing": append(bytes.Clone(good), 0),
		"row overflow": querySketchBlob(struct {
			key   int64
			count uint64
		}{0, math.MaxUint64}, struct {
			key   int64
			count uint64
		}{1, 1}),
	}
	for name, encoded := range badCases {
		t.Run(name, func(t *testing.T) {
			if _, err := latency.UnmarshalSketch(encoded); err == nil {
				t.Fatal("shared decoder accepted malformed sketch")
			}
			query := latency.NewQuerySketchMerger()
			if _, err := query.MergeBinary(good); err != nil {
				t.Fatal(err)
			}
			if _, err := query.MergeBinary(encoded); err == nil {
				t.Fatal("query merger accepted malformed sketch")
			}
			if query.TakeSketch() != nil {
				t.Fatal("failed query exposed a partially merged sketch")
			}
			if _, err := query.MergeBinary(good); err == nil {
				t.Fatal("failed query merger was reusable")
			}
		})
	}
	for name, second := range map[string][]byte{
		"bin overflow": querySketchBlob(struct {
			key   int64
			count uint64
		}{0, 1}),
		"count overflow": querySketchBlob(struct {
			key   int64
			count uint64
		}{1, 1}),
	} {
		t.Run(name, func(t *testing.T) {
			query := latency.NewQuerySketchMerger()
			first := querySketchBlob(struct {
				key   int64
				count uint64
			}{0, math.MaxUint64})
			if _, err := query.MergeBinary(first); err != nil {
				t.Fatal(err)
			}
			if _, err := query.MergeBinary(second); err == nil || query.TakeSketch() != nil {
				t.Fatal("overflow did not close and discard query sketch")
			}
		})
	}
}
