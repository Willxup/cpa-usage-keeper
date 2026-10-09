package latency

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	// MaxSamplePoints 是每个 Latency 聚合行允许保存的真实配对点上限。
	MaxSamplePoints = 1000
	// MaxEncodedSamplePoints 保留对已部署 2500 点 BLOB 的完整解码能力。
	MaxEncodedSamplePoints = 2500
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
	// 先把整批点加入 map 并检查冲突，最后统一裁剪；旧 BLOB 的尾部也必须参与检查。
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

// Count 返回当前保留的真实配对点数，不需要构造排序副本。
func (samples *SampleSet) Count() int {
	if samples == nil {
		return 0
	}
	return len(samples.points)
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
	if len(points) > MaxSamplePoints {
		return nil, fmt.Errorf("new sample encoding count %d exceeds limit %d", len(points), MaxSamplePoints)
	}
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
	samples := NewSampleSet()
	_, err := walkSampleSet(encoded, func(point SamplePoint) {
		samples.points[point.EventID] = point
	})
	if err != nil {
		return nil, err
	}
	return samples, nil
}

// QuerySampleMerger 独占查询中的样本集合和持续维护的最差点堆。
// 跨行冲突检查仅覆盖当前保留的 event ID；已淘汰 ID 不留历史记录。
// 与同上限的 SampleSet.Merge 一致，这不是对全部历史事件的重复/冲突审计。
// TakeSamples 后所有权交还调用方，此前不得通过其他路径修改内部集合。
type QuerySampleMerger struct {
	samples *SampleSet
	heap    []SamplePoint
}

func NewQuerySampleMerger() *QuerySampleMerger {
	return &QuerySampleMerger{samples: NewSampleSet()}
}

// MergeBinary 校验完整 BLOB 的格式及其与当前保留集合的冲突后才更改集合。
// 未入选的点仍经过格式校验，但已淘汰 ID 的跨行冲突不保证检测。
func (merger *QuerySampleMerger) MergeBinary(encoded []byte) (int, error) {
	if merger == nil || merger.samples == nil {
		return 0, fmt.Errorf("query sample merger is closed")
	}
	samples := merger.samples
	var candidates []SamplePoint
	var conflictEvent int64
	count, err := walkSampleSet(encoded, func(point SamplePoint) {
		// 严格落后于整行开始时的截止点，不可能与已保留 event ID 冲突。
		if len(merger.heap) == MaxSamplePoints && samplePointLess(merger.heap[0], point) {
			return
		}
		// 旧集合直到整行解析结束保持不变，因此即使该点随后会被裁剪，冲突也不会漏报。
		if previous, exists := samples.points[point.EventID]; exists {
			if previous != point && conflictEvent == 0 {
				conflictEvent = point.EventID
			}
			return
		}
		if len(merger.heap) < MaxSamplePoints || samplePointLess(point, merger.heap[0]) {
			candidates = append(candidates, point)
		}
	})
	if err != nil {
		return 0, err
	}
	if conflictEvent != 0 {
		return 0, fmt.Errorf("conflicting sample for event %d", conflictEvent)
	}
	if len(candidates) == 0 {
		return count, nil
	}
	// 整行格式与旧集合冲突均通过后才变更 map/heap；写入侧 SampleSet.trim 不参与查询。
	for _, point := range candidates {
		if len(merger.heap) < MaxSamplePoints {
			samples.points[point.EventID] = point
			merger.heap = append(merger.heap, point)
			for child := len(merger.heap) - 1; child > 0; {
				parent := (child - 1) / 2
				if !samplePointLess(merger.heap[parent], merger.heap[child]) {
					break
				}
				merger.heap[parent], merger.heap[child] = merger.heap[child], merger.heap[parent]
				child = parent
			}
		} else if samplePointLess(point, merger.heap[0]) {
			delete(samples.points, merger.heap[0].EventID)
			samples.points[point.EventID] = point
			merger.heap[0] = point
			siftDownWorst(merger.heap, 0)
		}
	}
	return count, nil
}

// TakeSamples 结束查询合并并转移集合所有权，阻止随后使用过期截止点。
func (merger *QuerySampleMerger) TakeSamples() *SampleSet {
	if merger == nil {
		return nil
	}
	samples := merger.samples
	merger.samples = nil
	merger.heap = nil
	return samples
}

// walkSampleSet 与写入端共用严格格式检查；匹配 hash 的 event ID 重复必然违反严格排序。
func walkSampleSet(encoded []byte, visit func(SamplePoint)) (int, error) {
	if len(encoded) == 0 {
		return 0, fmt.Errorf("sample data is empty")
	}
	if encoded[0] != FormatVersion {
		return 0, fmt.Errorf("unsupported sample version %d", encoded[0])
	}
	offset := 1
	count, err := readUvarint(encoded, &offset, "sample count")
	if err != nil {
		return 0, err
	}
	if count > MaxEncodedSamplePoints {
		return 0, fmt.Errorf("sample count %d exceeds limit %d", count, MaxEncodedSamplePoints)
	}
	var previous SamplePoint
	for index := uint64(0); index < count; index++ {
		eventID, err := readPositiveInt64(encoded, &offset, "sample event ID")
		if err != nil {
			return 0, err
		}
		priority, err := readUvarint(encoded, &offset, "sample priority")
		if err != nil {
			return 0, err
		}
		ttftMS, err := readPositiveInt64(encoded, &offset, "sample TTFT")
		if err != nil {
			return 0, err
		}
		latencyMS, err := readPositiveInt64(encoded, &offset, "sample latency")
		if err != nil {
			return 0, err
		}
		point := SamplePoint{EventID: eventID, Priority: priority, TTFTMS: ttftMS, LatencyMS: latencyMS}
		if priority != splitMix64(uint64(eventID)) {
			return 0, fmt.Errorf("sample priority does not match event %d", eventID)
		}
		if index > 0 && !samplePointLess(previous, point) {
			return 0, fmt.Errorf("sample points must be strictly ordered")
		}
		visit(point)
		previous = point
	}
	if offset != len(encoded) {
		return 0, fmt.Errorf("sample data has trailing bytes")
	}
	return int(count), nil
}

func (samples *SampleSet) trim() (SamplePoint, bool) {
	if len(samples.points) <= MaxSamplePoints {
		return SamplePoint{}, false
	}
	// 仅找出最小的 1000 点：最大堆堆顶始终是当前应保留集合中最差的点。
	// 比较键与 Points/旧版 trim 完全相同；最终输出时才进行稳定排序。
	points := make([]SamplePoint, 0, len(samples.points))
	for _, point := range samples.points {
		points = append(points, point)
	}
	heap := points[:MaxSamplePoints]
	for index := len(heap)/2 - 1; index >= 0; index-- {
		siftDownWorst(heap, index)
	}
	for _, point := range points[MaxSamplePoints:] {
		if samplePointLess(point, heap[0]) {
			delete(samples.points, heap[0].EventID)
			heap[0] = point
			siftDownWorst(heap, 0)
		} else {
			delete(samples.points, point.EventID)
		}
	}
	return heap[0], true
}

func siftDownWorst(heap []SamplePoint, parent int) {
	for {
		child := parent*2 + 1
		if child >= len(heap) {
			return
		}
		if right := child + 1; right < len(heap) && samplePointLess(heap[child], heap[right]) {
			child = right
		}
		if !samplePointLess(heap[parent], heap[child]) {
			return
		}
		heap[parent], heap[child] = heap[child], heap[parent]
		parent = child
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
