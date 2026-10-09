package latencystore

import (
	"fmt"
	"math"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// loadKeyBatchSize 限制三字段 composite IN 的参数数量和单次返回行数。
	loadKeyBatchSize = 200
)

// rowKey 使用可比较的存储时间字符串表示最终唯一键。
type rowKey struct {
	BucketType  entities.UsageLatencyBucketType
	BucketStart string
	APIGroupKey string
}

// decodedRow 只在单页内存合并期间存在，不逃逸为第二级缓存。
type decodedRow struct {
	TTFTSketch    *latency.Sketch
	LatencySketch *latency.Sketch
	SamplePoints  *latency.SampleSet
}

// DiagnosticsAggregate 是查询端合并后的只读诊断状态，不包含任何待写回字段。
type DiagnosticsAggregate struct {
	SampleCount   int64
	MaxTTFTMS     int64
	MaxLatencyMS  int64
	TTFTSketch    *latency.Sketch
	LatencySketch *latency.Sketch
	SamplePoints  *latency.SampleSet
}

// MergeDiagnosticsRows 严格解码并合并查询命中的 Latency rows；任一坏行都不暴露部分结果。
func MergeDiagnosticsRows(rows []entities.UsageLatencyStat) (DiagnosticsAggregate, error) {
	aggregate := DiagnosticsAggregate{}
	ttftMerger := latency.NewQuerySketchMerger()
	latencyMerger := latency.NewQuerySketchMerger()
	sampleMerger := latency.NewQuerySampleMerger()
	for index, row := range rows {
		if err := decodeQueryLatencyRow(row, sampleMerger, ttftMerger, latencyMerger); err != nil {
			return DiagnosticsAggregate{}, fmt.Errorf("decode latency diagnostics row %d: %w", index, err)
		}
		if aggregate.SampleCount > math.MaxInt64-row.SampleCount {
			return DiagnosticsAggregate{}, fmt.Errorf("sample count overflow")
		}
		aggregate.SampleCount += row.SampleCount
		aggregate.MaxTTFTMS = max(aggregate.MaxTTFTMS, row.MaxTTFTMS)
		aggregate.MaxLatencyMS = max(aggregate.MaxLatencyMS, row.MaxLatencyMS)
	}
	// 全部行成功后才转移查询私有状态；失败返回零聚合，不暴露部分合并值。
	aggregate.TTFTSketch = ttftMerger.TakeSketch()
	aggregate.LatencySketch = latencyMerger.TakeSketch()
	// 合并后再次核对全局计数，防止单行合法但跨行累计状态出现不一致。
	if aggregate.TTFTSketch.Count() != uint64(aggregate.SampleCount) || aggregate.LatencySketch.Count() != uint64(aggregate.SampleCount) {
		return DiagnosticsAggregate{}, fmt.Errorf("merged latency payload counts do not match sample_count %d", aggregate.SampleCount)
	}
	aggregate.SamplePoints = sampleMerger.TakeSamples()
	return aggregate, nil
}

// decodeQueryLatencyRow 直接校验编码样本并筛选候选点，避免为每个查询行构造 SampleSet map。
func decodeQueryLatencyRow(row entities.UsageLatencyStat, sampleMerger *latency.QuerySampleMerger, ttftMerger, latencyMerger *latency.QuerySketchMerger) error {
	if row.FormatVersion != latency.FormatVersion {
		return fmt.Errorf("unsupported format version %d", row.FormatVersion)
	}
	if row.SampleCount < 0 || row.MaxTTFTMS <= 0 || row.MaxLatencyMS <= 0 {
		return fmt.Errorf("invalid latency counters")
	}
	ttftCount, err := ttftMerger.MergeBinary(row.TTFTSketch)
	if err != nil {
		return fmt.Errorf("decode TTFT sketch: %w", err)
	}
	latencyCount, err := latencyMerger.MergeBinary(row.LatencySketch)
	if err != nil {
		return fmt.Errorf("decode latency sketch: %w", err)
	}
	count, err := sampleMerger.MergeBinary(row.SamplePoints)
	if err != nil {
		return fmt.Errorf("decode sample points: %w", err)
	}
	if ttftCount != uint64(row.SampleCount) || latencyCount != uint64(row.SampleCount) || int64(count) > row.SampleCount {
		return fmt.Errorf("latency payload counts do not match sample_count %d", row.SampleCount)
	}
	return nil
}

// ApplyRows 保留 migration 的同事务入口；运行时使用 PrepareRows 与 WritePreparedRows 分离 Reader/Writer。
func ApplyRows(tx *gorm.DB, rows []entities.UsageLatencyStat, now time.Time) error {
	prepared, err := PrepareRows(tx, rows, now)
	if err != nil {
		return err
	}
	return WritePreparedRows(tx, prepared)
}

// PrepareRows 批量读取旧唯一键，并在调用方提供的 Reader 上完成解码和内存合并。
func PrepareRows(db *gorm.DB, rows []entities.UsageLatencyStat, now time.Time) ([]entities.UsageLatencyStat, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if len(rows) == 0 {
		return []entities.UsageLatencyStat{}, nil
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 输入先按唯一键去重；同页重复 key 表示 BuildRows 契约被破坏，不能重复累计。
	keys := make([]rowKey, 0, len(rows))
	rowByKey := make(map[rowKey]entities.UsageLatencyStat, len(rows))
	for _, row := range rows {
		key := latencyRowKey(row)
		if _, exists := rowByKey[key]; exists {
			return nil, fmt.Errorf("duplicate latency row key %+v", key)
		}
		if _, err := decodeLatencyRow(row); err != nil {
			return nil, fmt.Errorf("decode incoming latency row %+v: %w", key, err)
		}
		keys = append(keys, key)
		rowByKey[key] = row
	}

	// 每块最多 200 个三字段 key，并在读取后立即完成合并，避免整页旧 BLOB 同时常驻内存。
	prepared := make([]entities.UsageLatencyStat, 0, len(keys))
	for start := 0; start < len(keys); start += loadKeyBatchSize {
		end := min(start+loadKeyBatchSize, len(keys))
		values := make([][]any, 0, end-start)
		for _, key := range keys[start:end] {
			values = append(values, []any{key.BucketType, key.BucketStart, key.APIGroupKey})
		}
		var existing []entities.UsageLatencyStat
		if err := db.Where("(bucket_type, bucket_start, api_group_key) IN ?", values).Find(&existing).Error; err != nil {
			return nil, fmt.Errorf("load latency rows [%d:%d]: %w", start, end, err)
		}
		existingByKey := make(map[rowKey]entities.UsageLatencyStat, len(existing))
		for _, row := range existing {
			existingByKey[latencyRowKey(row)] = row
		}
		// 当前块合并完成后 existing slice/map 即可回收；prepared 只保留最终编码结果。
		for _, key := range keys[start:end] {
			incoming := rowByKey[key]
			existing, exists := existingByKey[key]
			merged, err := mergeLatencyRows(existing, incoming, exists, normalizedNow)
			if err != nil {
				return nil, fmt.Errorf("merge latency row %+v: %w", key, err)
			}
			prepared = append(prepared, merged)
		}
	}
	return prepared, nil
}

// WritePreparedRows 只写已经合并完成的最终 BLOB；这里不得再查询或排序旧数据。
func WritePreparedRows(tx *gorm.DB, rows []entities.UsageLatencyStat) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	if len(rows) == 0 {
		return nil
	}
	seen := make(map[rowKey]struct{}, len(rows))
	for _, row := range rows {
		key := latencyRowKey(row)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate latency row key %+v", key)
		}
		decoded, err := decodeLatencyRow(row)
		if err != nil {
			return fmt.Errorf("decode prepared latency row %+v: %w", key, err)
		}
		if decoded.SamplePoints.Count() > latency.MaxSamplePoints {
			return fmt.Errorf("prepared latency row %+v has %d sample points above write limit %d", key, decoded.SamplePoints.Count(), latency.MaxSamplePoints)
		}
		seen[key] = struct{}{}
		if row.ID > 0 {
			// 只更新累计字段和 UpdatedAt，保留旧行 ID/CreatedAt。
			result := tx.Model(&entities.UsageLatencyStat{}).Where("id = ?", row.ID).Updates(map[string]any{
				"sample_count":   row.SampleCount,
				"max_ttft_ms":    row.MaxTTFTMS,
				"max_latency_ms": row.MaxLatencyMS,
				"format_version": row.FormatVersion,
				"ttft_sketch":    row.TTFTSketch,
				"latency_sketch": row.LatencySketch,
				"sample_points":  row.SamplePoints,
				"updated_at":     timeutil.FormatStorageTime(row.UpdatedAt),
			})
			if result.Error != nil {
				return fmt.Errorf("update latency row %d: %w", row.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("update latency row %d affected %d rows", row.ID, result.RowsAffected)
			}
			continue
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert latency row: %w", err)
		}
	}
	return nil
}

func latencyRowKey(row entities.UsageLatencyStat) rowKey {
	return rowKey{BucketType: row.BucketType, BucketStart: timeutil.FormatStorageTime(row.BucketStart), APIGroupKey: row.APIGroupKey}
}

func decodeLatencyRow(row entities.UsageLatencyStat) (decodedRow, error) {
	// 表版本和三个 BLOB 版本必须一致，避免部分升级后混合解析。
	if row.FormatVersion != latency.FormatVersion {
		return decodedRow{}, fmt.Errorf("unsupported format version %d", row.FormatVersion)
	}
	if row.SampleCount < 0 || row.MaxTTFTMS <= 0 || row.MaxLatencyMS <= 0 {
		return decodedRow{}, fmt.Errorf("invalid latency counters")
	}
	ttftSketch, err := latency.UnmarshalSketch(row.TTFTSketch)
	if err != nil {
		return decodedRow{}, fmt.Errorf("decode TTFT sketch: %w", err)
	}
	latencySketch, err := latency.UnmarshalSketch(row.LatencySketch)
	if err != nil {
		return decodedRow{}, fmt.Errorf("decode latency sketch: %w", err)
	}
	samplePoints, err := latency.UnmarshalSampleSet(row.SamplePoints)
	if err != nil {
		return decodedRow{}, fmt.Errorf("decode sample points: %w", err)
	}
	// 两个 Sketch 必须覆盖全部样本；兼容读取的旧 BLOB 最多 2500 点，新写入最多 1000 点。
	if ttftSketch.Count() != uint64(row.SampleCount) || latencySketch.Count() != uint64(row.SampleCount) || int64(samplePoints.Count()) > row.SampleCount {
		return decodedRow{}, fmt.Errorf("latency payload counts do not match sample_count %d", row.SampleCount)
	}
	return decodedRow{TTFTSketch: ttftSketch, LatencySketch: latencySketch, SamplePoints: samplePoints}, nil
}

func mergeLatencyRows(existing, incoming entities.UsageLatencyStat, hasExisting bool, now time.Time) (entities.UsageLatencyStat, error) {
	incomingDecoded, err := decodeLatencyRow(incoming)
	if err != nil {
		return entities.UsageLatencyStat{}, err
	}
	if !hasExisting {
		incoming.CreatedAt = now
		incoming.UpdatedAt = now
		return incoming, nil
	}
	existingDecoded, err := decodeLatencyRow(existing)
	if err != nil {
		return entities.UsageLatencyStat{}, err
	}
	if existing.SampleCount > math.MaxInt64-incoming.SampleCount {
		return entities.UsageLatencyStat{}, fmt.Errorf("sample count overflow")
	}
	if err := existingDecoded.TTFTSketch.Merge(incomingDecoded.TTFTSketch); err != nil {
		return entities.UsageLatencyStat{}, err
	}
	if err := existingDecoded.LatencySketch.Merge(incomingDecoded.LatencySketch); err != nil {
		return entities.UsageLatencyStat{}, err
	}
	if err := existingDecoded.SamplePoints.Merge(incomingDecoded.SamplePoints); err != nil {
		return entities.UsageLatencyStat{}, err
	}
	ttftSketch, err := existingDecoded.TTFTSketch.MarshalBinary()
	if err != nil {
		return entities.UsageLatencyStat{}, err
	}
	latencySketch, err := existingDecoded.LatencySketch.MarshalBinary()
	if err != nil {
		return entities.UsageLatencyStat{}, err
	}
	samplePoints, err := existingDecoded.SamplePoints.MarshalBinary()
	if err != nil {
		return entities.UsageLatencyStat{}, err
	}
	existing.SampleCount += incoming.SampleCount
	existing.MaxTTFTMS = max(existing.MaxTTFTMS, incoming.MaxTTFTMS)
	existing.MaxLatencyMS = max(existing.MaxLatencyMS, incoming.MaxLatencyMS)
	existing.FormatVersion = latency.FormatVersion
	existing.TTFTSketch = ttftSketch
	existing.LatencySketch = latencySketch
	existing.SamplePoints = samplePoints
	existing.UpdatedAt = now
	return existing, nil
}
