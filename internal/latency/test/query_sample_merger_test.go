package test

import (
	"bytes"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func TestQuerySampleMergerKeepsCutoffAcrossLosingRowsAndRefreshesAfterWinner(t *testing.T) {
	base := latency.NewSampleSet()
	for id := int64(1); id <= 10000; id++ {
		if err := base.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
	}
	baseBlob, _ := base.MarshalBinary()
	merger := latency.NewQuerySampleMerger()
	if _, err := merger.MergeBinary(baseBlob); err != nil {
		t.Fatal(err)
	}
	oracle := base.Clone()
	cutoff := oracle.Points()[latency.MaxSamplePoints-1]
	var losing [][]byte
	var winner []byte
	for id := int64(10001); id < 20000 && (len(losing) < 100 || winner == nil); id++ {
		one := latency.NewSampleSet()
		if err := one.Add(id, id+1, id+2); err != nil {
			t.Fatal(err)
		}
		point := one.Points()[0]
		blob, _ := one.MarshalBinary()
		if point.Priority > cutoff.Priority && len(losing) < 100 {
			losing = append(losing, blob)
		}
		if point.Priority < cutoff.Priority && winner == nil {
			winner = blob
		}
	}
	if len(losing) != 100 || winner == nil {
		t.Fatal("could not construct deterministic cutoff fixture")
	}
	for _, blob := range losing {
		if _, err := merger.MergeBinary(blob); err != nil {
			t.Fatal(err)
		}
		decoded, _ := latency.UnmarshalSampleSet(blob)
		if err := oracle.Merge(decoded); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := merger.MergeBinary(winner); err != nil {
		t.Fatal(err)
	}
	decodedWinner, _ := latency.UnmarshalSampleSet(winner)
	if err := oracle.Merge(decodedWinner); err != nil {
		t.Fatal(err)
	}
	for _, blob := range losing {
		if _, err := merger.MergeBinary(blob); err != nil {
			t.Fatal(err)
		}
	}
	got := merger.TakeSamples()
	gotBlob, _ := got.MarshalBinary()
	wantBlob, _ := oracle.MarshalBinary()
	if !bytes.Equal(gotBlob, wantBlob) {
		t.Fatal("cached cutoff changed retained BLOB")
	}
	if _, err := merger.MergeBinary(winner); err == nil {
		t.Fatal("merger accepted input after handing out samples")
	}
}
