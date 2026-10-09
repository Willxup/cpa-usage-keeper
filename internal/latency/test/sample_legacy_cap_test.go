package test

import (
	"encoding/binary"
	"reflect"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
)

// Encode the deployed 2500-point format without using SampleSet.Add, whose new cap is lower.
func legacySampleBlob(count int, corruptLast bool) ([]byte, []latency.SamplePoint) {
	points := make([]latency.SamplePoint, count)
	for index := range points {
		id := int64(index + 1)
		points[index] = latency.SamplePoint{EventID: id, Priority: legacyPriority(uint64(id)), TTFTMS: id + 10, LatencyMS: id + 100}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Priority != points[j].Priority {
			return points[i].Priority < points[j].Priority
		}
		return points[i].EventID < points[j].EventID
	})
	encoded := binary.AppendUvarint([]byte{latency.FormatVersion}, uint64(count))
	for index, point := range points {
		encoded = binary.AppendUvarint(encoded, uint64(point.EventID))
		encoded = binary.AppendUvarint(encoded, point.Priority)
		encoded = binary.AppendUvarint(encoded, uint64(point.TTFTMS))
		value := point.LatencyMS
		if corruptLast && index == count-1 {
			value = 0
		}
		encoded = binary.AppendUvarint(encoded, uint64(value))
	}
	return encoded, points
}

func legacyPriority(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func TestLegacy2500CodecRetainsFullRowAndQueryKeepsTop1000(t *testing.T) {
	blob, ordered := legacySampleBlob(2500, false)
	decoded, err := latency.UnmarshalSampleSet(blob)
	if err != nil {
		t.Fatalf("decode deployed 2500-point BLOB: %v", err)
	}
	if decoded.Count() != 2500 || !reflect.DeepEqual(decoded.Points(), ordered) {
		t.Fatal("legacy decode lost points before storage merge can validate conflicts")
	}
	merger := latency.NewQuerySampleMerger()
	count, err := merger.MergeBinary(blob)
	if err != nil || count != 2500 {
		t.Fatalf("query merge legacy BLOB: count=%d err=%v", count, err)
	}
	if got := merger.TakeSamples().Points(); !reflect.DeepEqual(got, ordered[:1000]) {
		t.Fatalf("query retained %d points, want first 1000", len(got))
	}
	bad, _ := legacySampleBlob(2500, true)
	if _, err := latency.UnmarshalSampleSet(bad); err == nil {
		t.Fatal("legacy decoder accepted corrupt point after the new cap")
	}
	if _, err := latency.NewQuerySampleMerger().MergeBinary(bad); err == nil {
		t.Fatal("query merger accepted corrupt point after the new cap")
	}
}

func TestNewSampleAndHourDayRowsKeepAtMost1000(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	samples := latency.NewSampleSet()
	events := make([]entities.UsageEvent, 1600)
	for index := range events {
		id := int64(index + 1)
		if err := samples.Add(id, id+10, id+100); err != nil {
			t.Fatal(err)
		}
		ttft := id + 10
		generate := true
		events[index] = entities.UsageEvent{ID: id, Timestamp: now.Add(-time.Minute), Generate: &generate, TTFTMS: &ttft, LatencyMS: id + 100}
	}
	if samples.Count() != 1000 {
		t.Fatalf("new SampleSet retained %d points, want 1000", samples.Count())
	}
	rows, err := latency.BuildRows(events, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one hour and one day row, got %d", len(rows))
	}
	for _, row := range rows {
		decoded, err := latency.UnmarshalSampleSet(row.SamplePoints)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Count() != 1000 || row.SampleCount != 1600 {
			t.Fatalf("%s row has stored=%d total=%d", row.BucketType, decoded.Count(), row.SampleCount)
		}
	}
}
