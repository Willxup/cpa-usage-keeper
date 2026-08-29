package latency

import (
	"fmt"
	"sort"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

// aggregateKey 与 usage_latency_stats 的最终唯一键完全一致。
type aggregateKey struct {
	BucketType  entities.UsageLatencyBucketType
	BucketStart time.Time
	APIGroupKey string
}

// aggregateRowBuilder 保存尚未编码的一页内聚合状态。
type aggregateRowBuilder struct {
	SampleCount   int64
	MaxTTFTMS     int64
	MaxLatencyMS  int64
	TTFTSketch    *Sketch
	LatencySketch *Sketch
	SamplePoints  *SampleSet
}

const (
	// HourRetentionDays 为最长 24 小时查询保留三天 hourly 数据。
	HourRetentionDays = 3
	// DayRetentionDays 覆盖页面允许选择的最近 365 个自然日。
	DayRetentionDays = 365
)

// RetentionBucketStartCutoff 返回仍应保留的最早 bucket_start。
func RetentionBucketStartCutoff(bucketType entities.UsageLatencyBucketType, now time.Time) (time.Time, error) {
	if now.IsZero() {
		return time.Time{}, fmt.Errorf("latency retention time is zero")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	switch bucketType {
	case entities.UsageLatencyBucketHour:
		// 按本地钟面减去分秒，避免 time.Truncate 在非整小时 UTC 偏移下产生 xx:15/xx:45 桶。
		return localHourStart(normalizedNow.Add(-HourRetentionDays * 24 * time.Hour)), nil
	case entities.UsageLatencyBucketDay:
		// 365 个自然日包含今天，因此最早一天是本地今天向前 364 天。
		today := time.Date(normalizedNow.Year(), normalizedNow.Month(), normalizedNow.Day(), 0, 0, 0, 0, normalizedNow.Location())
		return today.AddDate(0, 0, -(DayRetentionDays - 1)), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported latency bucket type %q", bucketType)
	}
}

// BuildRows 把有效 generate 事件同时聚合成同表 hour/day rows，输入切片只读。
func BuildRows(events []entities.UsageEvent, now time.Time) ([]entities.UsageLatencyStat, error) {
	hourCutoff, err := RetentionBucketStartCutoff(entities.UsageLatencyBucketHour, now)
	if err != nil {
		return nil, err
	}
	dayCutoff, err := RetentionBucketStartCutoff(entities.UsageLatencyBucketDay, now)
	if err != nil {
		return nil, err
	}
	builders := make(map[aggregateKey]*aggregateRowBuilder)
	for _, event := range events {
		// 失败、非 generate、缺少正 TTFT 或非正总延迟都不是 Latency 样本。
		if event.Failed || event.Generate == nil || !*event.Generate || event.TTFTMS == nil || *event.TTFTMS <= 0 || event.LatencyMS <= 0 {
			continue
		}
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		apiGroupKey := activity.NormalizeAPIGroupKey(event.APIGroupKey)
		hourStart := localHourStart(timestamp)
		dayStart := time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
		for _, key := range []aggregateKey{
			{BucketType: entities.UsageLatencyBucketHour, BucketStart: hourStart, APIGroupKey: apiGroupKey},
			{BucketType: entities.UsageLatencyBucketDay, BucketStart: dayStart, APIGroupKey: apiGroupKey},
		} {
			// checkpoint 仍覆盖全部事件 ID；只跳过页面已经无法查询的过期存储粒度。
			if key.BucketType == entities.UsageLatencyBucketHour && key.BucketStart.Before(hourCutoff) {
				continue
			}
			if key.BucketType == entities.UsageLatencyBucketDay && key.BucketStart.Before(dayCutoff) {
				continue
			}
			builder := builders[key]
			if builder == nil {
				builder = &aggregateRowBuilder{TTFTSketch: NewSketch(), LatencySketch: NewSketch(), SamplePoints: NewSampleSet()}
				builders[key] = builder
			}
			if err := builder.add(event.ID, *event.TTFTMS, event.LatencyMS); err != nil {
				return nil, fmt.Errorf("build latency row for event %d: %w", event.ID, err)
			}
		}
	}

	// map 转最终行时编码一次；同一页内不反复序列化中间状态。
	rows := make([]entities.UsageLatencyStat, 0, len(builders))
	for key, builder := range builders {
		ttftSketch, err := builder.TTFTSketch.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode TTFT sketch: %w", err)
		}
		latencySketch, err := builder.LatencySketch.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode latency sketch: %w", err)
		}
		samplePoints, err := builder.SamplePoints.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode latency sample points: %w", err)
		}
		rows = append(rows, entities.UsageLatencyStat{
			BucketType: key.BucketType, BucketStart: key.BucketStart, APIGroupKey: key.APIGroupKey,
			SampleCount: builder.SampleCount, MaxTTFTMS: builder.MaxTTFTMS, MaxLatencyMS: builder.MaxLatencyMS,
			FormatVersion: FormatVersion, TTFTSketch: ttftSketch, LatencySketch: latencySketch, SamplePoints: samplePoints,
		})
	}
	// 固定写入顺序让 migration/runtime 和失败日志保持可复核。
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].BucketType != rows[right].BucketType {
			return rows[left].BucketType < rows[right].BucketType
		}
		if !rows[left].BucketStart.Equal(rows[right].BucketStart) {
			return rows[left].BucketStart.Before(rows[right].BucketStart)
		}
		return rows[left].APIGroupKey < rows[right].APIGroupKey
	})
	return rows, nil
}

func localHourStart(timestamp time.Time) time.Time {
	return timestamp.Add(-time.Duration(timestamp.Minute())*time.Minute - time.Duration(timestamp.Second())*time.Second - time.Duration(timestamp.Nanosecond()))
}

func (builder *aggregateRowBuilder) add(eventID, ttftMS, latencyMS int64) error {
	// 三种表示必须共同成功；任一错误都让整页 BuildRows 失败并保持零写入。
	if err := builder.TTFTSketch.Add(ttftMS); err != nil {
		return err
	}
	if err := builder.LatencySketch.Add(latencyMS); err != nil {
		return err
	}
	if err := builder.SamplePoints.Add(eventID, ttftMS, latencyMS); err != nil {
		return err
	}
	builder.SampleCount++
	if ttftMS > builder.MaxTTFTMS {
		builder.MaxTTFTMS = ttftMS
	}
	if latencyMS > builder.MaxLatencyMS {
		builder.MaxLatencyMS = latencyMS
	}
	return nil
}
