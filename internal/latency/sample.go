package latency

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	// MaxSamplePoints 是每个 Latency 聚合行允许保存的真实配对点上限。
	MaxSamplePoints = 2500
)

// SamplePoint 保存同一请求的 TTFT/Latency 配对，priority 只用于稳定抽样。
type SamplePoint struct {
	EventID   int64
	Priority  uint64
	TTFTMS    int64
	LatencyMS int64
}

// SampleSet 按 event ID 去重，并始终保留全局优先级最小的有界集合。
type SampleSet struct {
	points map[int64]SamplePoint
}

// NewSampleSet 创建空的稳定样本集合。
func NewSampleSet() *SampleSet {
	return &SampleSet{points: make(map[int64]SamplePoint)}
}

// Add 加入一条真实配对点；冲突 event ID 返回错误以保持合并顺序无关。
func (samples *SampleSet) Add(eventID, ttftMS, latencyMS int64) error {
	if samples == nil {
		return fmt.Errorf("sample set is nil")
	}
	if eventID <= 0 || ttftMS <= 0 || latencyMS <= 0 {
		return fmt.Errorf("sample values must be positive: event=%d ttft=%d latency=%d", eventID, ttftMS, latencyMS)
	}
	if samples.points == nil {
		samples.points = make(map[int64]SamplePoint)
	}
	point := SamplePoint{EventID: eventID, Priority: splitMix64(uint64(eventID)), TTFTMS: ttftMS, LatencyMS: latencyMS}
	if err := samples.addPoint(point); err != nil {
		return err
	}
	samples.trim()
	return nil
}

// Merge 合并另一个集合；精确重复去重，冲突重复失败。
func (samples *SampleSet) Merge(other *SampleSet) error {
	if samples == nil || other == nil {
		return fmt.Errorf("sample set is nil")
	}
	if samples.points == nil {
		samples.points = make(map[int64]SamplePoint)
	}
	// 先把整批点加入 map，最后统一排序和裁剪；旧集合已满时不再为每个新点重复排序 2500 项。
	for _, point := range other.points {
		if err := samples.addPoint(point); err != nil {
			return err
		}
	}
	samples.trim()
	return nil
}

func (samples *SampleSet) addPoint(point SamplePoint) error {
	if existing, exists := samples.points[point.EventID]; exists {
		if existing != point {
			return fmt.Errorf("conflicting sample for event %d", point.EventID)
		}
		return nil
	}
	samples.points[point.EventID] = point
	return nil
}

// Clone 返回没有共享 map 的副本。
func (samples *SampleSet) Clone() *SampleSet {
	clone := NewSampleSet()
	if samples == nil {
		return clone
	}
	for eventID, point := range samples.points {
		clone.points[eventID] = point
	}
	return clone
}

// Points 按 priority、event ID 返回稳定副本。
func (samples *SampleSet) Points() []SamplePoint {
	if samples == nil {
		return nil
	}
	points := make([]SamplePoint, 0, len(samples.points))
	for _, point := range samples.points {
		points = append(points, point)
	}
	sort.Slice(points, func(left, right int) bool {
		return samplePointLess(points[left], points[right])
	})
	return points
}

// MarshalBinary 输出版本、点数和固定字段 uvarint，禁止无界 JSON。
func (samples *SampleSet) MarshalBinary() ([]byte, error) {
	if samples == nil {
		return nil, fmt.Errorf("sample set is nil")
	}
	points := samples.Points()
	encoded := []byte{FormatVersion}
	encoded = binary.AppendUvarint(encoded, uint64(len(points)))
	for _, point := range points {
		encoded = binary.AppendUvarint(encoded, uint64(point.EventID))
		encoded = binary.AppendUvarint(encoded, point.Priority)
		encoded = binary.AppendUvarint(encoded, uint64(point.TTFTMS))
		encoded = binary.AppendUvarint(encoded, uint64(point.LatencyMS))
	}
	return encoded, nil
}

// UnmarshalSampleSet 严格验证版本、边界、顺序、priority 和重复 event ID。
func UnmarshalSampleSet(encoded []byte) (*SampleSet, error) {
	if len(encoded) == 0 {
		return nil, fmt.Errorf("sample data is empty")
	}
	if encoded[0] != FormatVersion {
		return nil, fmt.Errorf("unsupported sample version %d", encoded[0])
	}
	offset := 1
	count, err := readUvarint(encoded, &offset, "sample count")
	if err != nil {
		return nil, err
	}
	if count > MaxSamplePoints {
		return nil, fmt.Errorf("sample count %d exceeds limit %d", count, MaxSamplePoints)
	}
	samples := NewSampleSet()
	var previous SamplePoint
	for index := uint64(0); index < count; index++ {
		eventID, err := readPositiveInt64(encoded, &offset, "sample event ID")
		if err != nil {
			return nil, err
		}
		priority, err := readUvarint(encoded, &offset, "sample priority")
		if err != nil {
			return nil, err
		}
		ttftMS, err := readPositiveInt64(encoded, &offset, "sample TTFT")
		if err != nil {
			return nil, err
		}
		latencyMS, err := readPositiveInt64(encoded, &offset, "sample latency")
		if err != nil {
			return nil, err
		}
		point := SamplePoint{EventID: eventID, Priority: priority, TTFTMS: ttftMS, LatencyMS: latencyMS}
		if priority != splitMix64(uint64(eventID)) {
			return nil, fmt.Errorf("sample priority does not match event %d", eventID)
		}
		if index > 0 && !samplePointLess(previous, point) {
			return nil, fmt.Errorf("sample points must be strictly ordered")
		}
		if _, exists := samples.points[eventID]; exists {
			return nil, fmt.Errorf("duplicate sample event %d", eventID)
		}
		samples.points[eventID] = point
		previous = point
	}
	if offset != len(encoded) {
		return nil, fmt.Errorf("sample data has trailing bytes")
	}
	return samples, nil
}

func (samples *SampleSet) trim() {
	if len(samples.points) <= MaxSamplePoints {
		return
	}
	// Points 已按保留优先级排序，删除尾部即可得到全局最小固定集合。
	points := samples.Points()
	for _, point := range points[MaxSamplePoints:] {
		delete(samples.points, point.EventID)
	}
}

func samplePointLess(left, right SamplePoint) bool {
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	return left.EventID < right.EventID
}

func splitMix64(value uint64) uint64 {
	// 固定 SplitMix64 只依赖 event ID，同一事件在任意进程都得到同一抽样优先级。
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func readPositiveInt64(encoded []byte, offset *int, field string) (int64, error) {
	value, err := readUvarint(encoded, offset, field)
	if err != nil {
		return 0, err
	}
	if value == 0 || value > math.MaxInt64 {
		return 0, fmt.Errorf("%s must fit positive int64", field)
	}
	return int64(value), nil
}
