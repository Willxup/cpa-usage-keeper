package test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"cpa-usage-keeper/internal/latency"
)

func TestSketchBinaryRoundTripKeepsDeterministicOnePercentP95(t *testing.T) {
	// 1..1000 的 nearest-rank P95 是手工可得的 950，适合验证误差上限。
	sketch := latency.NewSketch()
	for value := int64(1); value <= 1000; value++ {
		if err := sketch.Add(value); err != nil {
			t.Fatalf("add sketch value %d: %v", value, err)
		}
	}

	// 编码、解码、再次编码必须完全一致，确保不同进程和合并顺序得到稳定 BLOB。
	encoded, err := sketch.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal sketch: %v", err)
	}
	decoded, err := latency.UnmarshalSketch(encoded)
	if err != nil {
		t.Fatalf("unmarshal sketch: %v", err)
	}
	reencoded, err := decoded.MarshalBinary()
	if err != nil {
		t.Fatalf("remarshal sketch: %v", err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("sketch encoding changed after round trip")
	}

	// 对固定分布验证 P95 相对误差不超过计划约定的 1%。
	p95 := decoded.P95()
	if relativeError := math.Abs(float64(p95-950)) / 950; relativeError > 0.01 {
		t.Fatalf("expected p95 within 1%% of 950, got %d (error %.4f)", p95, relativeError)
	}
	if decoded.Count() != 1000 {
		t.Fatalf("expected sketch count 1000, got %d", decoded.Count())
	}
}

func TestSketchMergeOrderProducesSameEncoding(t *testing.T) {
	// 两个输入集合分别构建，模拟 store 合并旧 BLOB 与新批次 BLOB。
	left := latency.NewSketch()
	right := latency.NewSketch()
	for _, value := range []int64{1, 10, 100, 1000} {
		if err := left.Add(value); err != nil {
			t.Fatalf("add left value: %v", err)
		}
	}
	for _, value := range []int64{2, 20, 200, 2000} {
		if err := right.Add(value); err != nil {
			t.Fatalf("add right value: %v", err)
		}
	}

	// left+right 与 right+left 必须得到同一有序桶编码。
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
		t.Fatalf("marshal left-first sketch: %v", err)
	}
	rightBytes, err := rightFirst.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal right-first sketch: %v", err)
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		t.Fatalf("sketch merge order changed encoding")
	}
}

func TestUnmarshalSketchRejectsUnknownTruncatedAndUnsortedData(t *testing.T) {
	// 未知版本不能按当前格式猜测解析。
	if _, err := latency.UnmarshalSketch([]byte{2, 0}); err == nil {
		t.Fatal("expected unknown sketch version to fail")
	}

	// 声明一个桶却不给 key/count，必须报告截断而不是返回空 sketch。
	if _, err := latency.UnmarshalSketch([]byte{latency.FormatVersion, 1}); err == nil {
		t.Fatal("expected truncated sketch to fail")
	}

	// 手工构造 key=2 后 key=1，证明解码拒绝乱序或重复桶。
	encoded := []byte{latency.FormatVersion}
	encoded = binary.AppendUvarint(encoded, 2)
	encoded = binary.AppendUvarint(encoded, 4)
	encoded = binary.AppendUvarint(encoded, 1)
	encoded = binary.AppendUvarint(encoded, 2)
	encoded = binary.AppendUvarint(encoded, 1)
	if _, err := latency.UnmarshalSketch(encoded); err == nil {
		t.Fatal("expected unsorted sketch bins to fail")
	}
}

func TestEmptySketchReturnsZeroP95(t *testing.T) {
	// 空集合没有延迟分位值，固定返回 0 而不是 NaN 或错误。
	if got := latency.NewSketch().P95(); got != 0 {
		t.Fatalf("expected empty p95 0, got %d", got)
	}
}
