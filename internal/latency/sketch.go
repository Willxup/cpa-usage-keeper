package latency

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	// FormatVersion 同时标记 Sketch、SamplePoints 和表行的当前二进制格式。
	FormatVersion = 1
	// RelativeAccuracy 是正数对数桶两端允许的最大相对误差。
	RelativeAccuracy = 0.01
)

var (
	// sketchGamma 把 1% 相对误差转换成固定对数桶倍率。
	sketchGamma = (1 + RelativeAccuracy) / (1 - RelativeAccuracy)
	// sketchLogGamma 只计算一次，避免每个样本重复求固定倍率的对数。
	sketchLogGamma = math.Log(sketchGamma)
)

// Sketch 用对数桶保存正整数延迟分布；map 只参与累计，编码时固定排序。
type Sketch struct {
	bins  map[int64]uint64
	count uint64
}

// NewSketch 创建当前版本的空 Sketch。
func NewSketch() *Sketch {
	return &Sketch{bins: make(map[int64]uint64)}
}

// Add 把一个正毫秒值加入对应对数桶。
func (sketch *Sketch) Add(value int64) error {
	if sketch == nil {
		return fmt.Errorf("sketch is nil")
	}
	if value <= 0 {
		return fmt.Errorf("sketch value must be positive: %d", value)
	}
	if sketch.bins == nil {
		sketch.bins = make(map[int64]uint64)
	}
	// ceil(log(value)/log(gamma)) 保证 value 不超过桶上界。
	key := int64(math.Ceil(math.Log(float64(value)) / sketchLogGamma))
	if sketch.bins[key] == math.MaxUint64 || sketch.count == math.MaxUint64 {
		return fmt.Errorf("sketch count overflow")
	}
	sketch.bins[key]++
	sketch.count++
	return nil
}

// Merge 把另一个当前格式 Sketch 累加进接收者，结果与合并顺序无关。
func (sketch *Sketch) Merge(other *Sketch) error {
	if sketch == nil || other == nil {
		return fmt.Errorf("sketch is nil")
	}
	if sketch.count > math.MaxUint64-other.count {
		return fmt.Errorf("sketch count overflow")
	}
	if sketch.bins == nil {
		sketch.bins = make(map[int64]uint64)
	}
	for key, count := range other.bins {
		if sketch.bins[key] > math.MaxUint64-count {
			return fmt.Errorf("sketch bin %d overflow", key)
		}
		sketch.bins[key] += count
	}
	sketch.count += other.count
	return nil
}

// Clone 返回与原对象没有共享 map 的副本，供不同合并顺序测试和 store 使用。
func (sketch *Sketch) Clone() *Sketch {
	clone := NewSketch()
	if sketch == nil {
		return clone
	}
	clone.count = sketch.count
	for key, count := range sketch.bins {
		clone.bins[key] = count
	}
	return clone
}

// Count 返回所有桶累计的样本数。
func (sketch *Sketch) Count() uint64 {
	if sketch == nil {
		return 0
	}
	return sketch.count
}

// P95 使用 nearest-rank 在有序桶中定位，并返回桶的固定代表值。
func (sketch *Sketch) P95() int64 {
	if sketch == nil || sketch.count == 0 {
		return 0
	}
	// 整数公式 ceil(count*95/100) 避免浮点 rank 在大计数下失真。
	rank := (sketch.count*95 + 99) / 100
	keys := sketch.sortedKeys()
	cumulative := uint64(0)
	for _, key := range keys {
		cumulative += sketch.bins[key]
		if cumulative >= rank {
			return sketchRepresentativeValue(key)
		}
	}
	// count 与 bins 若不一致属于内部损坏；防御性返回最后一个桶而不是 panic。
	return sketchRepresentativeValue(keys[len(keys)-1])
}

// MarshalBinary 按版本、桶数、升序 key/count 输出确定性二进制。
func (sketch *Sketch) MarshalBinary() ([]byte, error) {
	if sketch == nil {
		return nil, fmt.Errorf("sketch is nil")
	}
	encoded := []byte{FormatVersion}
	keys := sketch.sortedKeys()
	encoded = binary.AppendUvarint(encoded, uint64(len(keys)))
	for _, key := range keys {
		count := sketch.bins[key]
		if count == 0 {
			return nil, fmt.Errorf("sketch bin %d has zero count", key)
		}
		encoded = binary.AppendUvarint(encoded, zigzagEncode(key))
		encoded = binary.AppendUvarint(encoded, count)
	}
	return encoded, nil
}

// UnmarshalSketch 严格解析当前版本，拒绝未知版本、乱序、重复、截断和溢出。
func UnmarshalSketch(encoded []byte) (*Sketch, error) {
	if len(encoded) == 0 {
		return nil, fmt.Errorf("sketch data is empty")
	}
	if encoded[0] != FormatVersion {
		return nil, fmt.Errorf("unsupported sketch version %d", encoded[0])
	}
	offset := 1
	binCount, err := readUvarint(encoded, &offset, "sketch bin count")
	if err != nil {
		return nil, err
	}
	sketch := NewSketch()
	previousKey := int64(0)
	for index := uint64(0); index < binCount; index++ {
		encodedKey, err := readUvarint(encoded, &offset, "sketch bin key")
		if err != nil {
			return nil, err
		}
		key := zigzagDecode(encodedKey)
		if index > 0 && key <= previousKey {
			return nil, fmt.Errorf("sketch bin keys must be strictly increasing")
		}
		count, err := readUvarint(encoded, &offset, "sketch bin count")
		if err != nil {
			return nil, err
		}
		if count == 0 || sketch.count > math.MaxUint64-count {
			return nil, fmt.Errorf("invalid sketch bin count %d", count)
		}
		sketch.bins[key] = count
		sketch.count += count
		previousKey = key
	}
	if offset != len(encoded) {
		return nil, fmt.Errorf("sketch data has trailing bytes")
	}
	return sketch, nil
}

func (sketch *Sketch) sortedKeys() []int64 {
	keys := make([]int64, 0, len(sketch.bins))
	for key := range sketch.bins {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	return keys
}

func sketchRepresentativeValue(key int64) int64 {
	// 桶上界与 gamma 共同确定两端等误差的代表值。
	upper := math.Pow(sketchGamma, float64(key))
	representative := math.Round(2 * upper / (sketchGamma + 1))
	if representative < 1 {
		return 1
	}
	if representative > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(representative)
}

func zigzagEncode(value int64) uint64 {
	return uint64(value<<1) ^ uint64(value>>63)
}

func zigzagDecode(value uint64) int64 {
	return int64(value>>1) ^ -int64(value&1)
}

func readUvarint(encoded []byte, offset *int, field string) (uint64, error) {
	if offset == nil || *offset >= len(encoded) {
		return 0, fmt.Errorf("%s is truncated", field)
	}
	value, size := binary.Uvarint(encoded[*offset:])
	if size == 0 {
		return 0, fmt.Errorf("%s is truncated", field)
	}
	if size < 0 {
		return 0, fmt.Errorf("%s overflows uint64", field)
	}
	*offset += size
	return value, nil
}
