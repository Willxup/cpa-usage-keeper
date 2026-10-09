package latency

import (
	"fmt"
	"math"
)

// QuerySketchMerger 独占查询中的 Sketch；发生错误后丢弃部分累计值并关闭。
// 只有全部行成功时，调用方才能用 TakeSketch 移交结果。
type QuerySketchMerger struct {
	sketch *Sketch
}

func NewQuerySketchMerger() *QuerySketchMerger {
	return &QuerySketchMerger{sketch: NewSketch()}
}

// MergeBinary 复用普通解码的严格 walker，直接把合法 bin 累计到查询私有 Sketch。
func (merger *QuerySketchMerger) MergeBinary(encoded []byte) (uint64, error) {
	if merger == nil || merger.sketch == nil {
		return 0, fmt.Errorf("query sketch merger is closed")
	}
	var overflowKey int64
	binOverflow := false
	rowCount, err := walkSketch(encoded, func(key int64, count uint64) {
		// 即使累计值溢出也让 walker 继续验证整行，之后直接丢弃私有状态。
		if binOverflow {
			return
		}
		if merger.sketch.bins[key] > math.MaxUint64-count {
			overflowKey = key
			binOverflow = true
			return
		}
		merger.sketch.bins[key] += count
	})
	if err != nil {
		merger.sketch = nil
		return 0, err
	}
	if binOverflow {
		merger.sketch = nil
		return 0, fmt.Errorf("sketch bin %d overflow", overflowKey)
	}
	if merger.sketch.count > math.MaxUint64-rowCount {
		merger.sketch = nil
		return 0, fmt.Errorf("sketch count overflow")
	}
	merger.sketch.count += rowCount
	return rowCount, nil
}

// TakeSketch 结束查询并转移所有权；失败或已经移交时返回 nil。
func (merger *QuerySketchMerger) TakeSketch() *Sketch {
	if merger == nil {
		return nil
	}
	sketch := merger.sketch
	merger.sketch = nil
	return sketch
}
