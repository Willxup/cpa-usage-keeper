package test

import (
	"bytes"
	"math"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func TestSampleSetKeepsStableGlobalPrioritySubsetAcrossMergeOrders(t *testing.T) {
	// 两半数据合计超过 2500，最终必须只保留全局固定优先级最小集合。
	left := latency.NewSampleSet()
	right := latency.NewSampleSet()
	for eventID := int64(1); eventID <= 3000; eventID++ {
		target := left
		if eventID%2 == 0 {
			target = right
		}
		if err := target.Add(eventID, 100+eventID, 500+eventID); err != nil {
			t.Fatalf("add sample %d: %v", eventID, err)
		}
	}

	// 两种合并顺序都必须裁剪成同一 2500 点二进制结果。
	leftFirst := left.Clone()
	if err := leftFirst.Merge(right); err != nil {
		t.Fatalf("merge right into left: %v", err)
	}
	rightFirst := right.Clone()
	if err := rightFirst.Merge(left); err != nil {
		t.Fatalf("merge left into right: %v", err)
	}
	leftBytes, err := leftFirst.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal left-first samples: %v", err)
	}
	rightBytes, err := rightFirst.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal right-first samples: %v", err)
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		t.Fatal("sample merge order changed retained points")
	}
	if len(leftFirst.Points()) != latency.MaxSamplePoints {
		t.Fatalf("expected %d retained points, got %d", latency.MaxSamplePoints, len(leftFirst.Points()))
	}
	if len(leftBytes) >= 100*1024 {
		t.Fatalf("expected bounded sample encoding below 100 KiB, got %d bytes", len(leftBytes))
	}

	// 解码后仍保持 priority、event ID 的严格稳定顺序。
	decoded, err := latency.UnmarshalSampleSet(leftBytes)
	if err != nil {
		t.Fatalf("unmarshal retained samples: %v", err)
	}
	points := decoded.Points()
	for index := 1; index < len(points); index++ {
		previous := points[index-1]
		current := points[index]
		if previous.Priority > current.Priority || (previous.Priority == current.Priority && previous.EventID >= current.EventID) {
			t.Fatalf("sample points not stably ordered at %d: prev=%+v current=%+v", index, previous, current)
		}
	}
}

func TestSampleSetDeduplicatesExactEventAndRejectsConflictingEvent(t *testing.T) {
	// 同一事件重复出现在重试页时应去重，完全相同的数据不改变集合。
	samples := latency.NewSampleSet()
	if err := samples.Add(7, 120, 900); err != nil {
		t.Fatalf("add first sample: %v", err)
	}
	if err := samples.Add(7, 120, 900); err != nil {
		t.Fatalf("deduplicate exact sample: %v", err)
	}
	if len(samples.Points()) != 1 {
		t.Fatalf("expected one deduplicated point, got %+v", samples.Points())
	}

	// 同一 event ID 带不同延迟表示数据损坏；静默选择一方会让合并结果依赖顺序。
	if err := samples.Add(7, 121, 900); err == nil {
		t.Fatal("expected conflicting duplicate sample to fail")
	}
}

func TestSampleSetRejectsInvalidAndUnknownBinaryData(t *testing.T) {
	// event ID 与两种延迟都必须为正数，避免无意义点污染散点图。
	samples := latency.NewSampleSet()
	if err := samples.Add(0, 100, 500); err == nil {
		t.Fatal("expected zero event ID to fail")
	}
	if err := samples.Add(1, 0, 500); err == nil {
		t.Fatal("expected zero TTFT to fail")
	}
	if err := samples.Add(1, 100, 0); err == nil {
		t.Fatal("expected zero latency to fail")
	}
	if _, err := latency.UnmarshalSampleSet([]byte{2, 0}); err == nil {
		t.Fatal("expected unknown sample version to fail")
	}

	// 最大整数宽度下 2500 点仍必须低于计划约定的 100 KiB。
	wide := latency.NewSampleSet()
	for offset := int64(0); offset < latency.MaxSamplePoints; offset++ {
		if err := wide.Add(math.MaxInt64-offset, math.MaxInt64-offset, math.MaxInt64-offset); err != nil {
			t.Fatalf("add maximum-width sample %d: %v", offset, err)
		}
	}
	encoded, err := wide.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal maximum-width samples: %v", err)
	}
	if len(encoded) >= 100*1024 {
		t.Fatalf("maximum-width sample encoding is %d bytes", len(encoded))
	}
}
