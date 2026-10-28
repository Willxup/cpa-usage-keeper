package repository

import (
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// usageBucketRead 只解释既有 Overview 存储键。真实日界不能直接替代旧日桶键，
// 因此查询用 keys，返回的日序列用 displayStarts；两者都不写回数据库。
type usageBucketRead struct {
	keys          []string
	displayStarts map[int64]time.Time
	start, end    time.Time
	stableOffset  bool
}

func overviewBucketRead(start, end time.Time, grain string) usageBucketRead {
	start, end = timeutil.NormalizeStorageTime(start), timeutil.NormalizeStorageTime(end)
	result := usageBucketRead{displayStarts: make(map[int64]time.Time), start: start, end: end, stableOffset: usageReadHasStableOffset(start, end)}
	if result.stableOffset {
		return result
	}
	if grain == "daily" {
		for date := timeutil.DateOf(start); ; date = date.AddDays(1) {
			bounds, exists := timeutil.DayBounds(date, start.Location())
			if !exists {
				continue
			}
			if !bounds.Start.Before(end) {
				break
			}
			if bounds.Start.Before(start) {
				continue
			}
			// 调用现有纯归桶函数，保持历史 midnight gap/fold 的存储规则。
			_, key := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: bounds.Start})
			result.keys = append(result.keys, timeutil.FormatStorageTime(key.BucketStart))
			result.displayStarts[key.BucketStart.Unix()] = bounds.Start
		}
		return result
	}
	// Overview 按绝对整小时分桶，不能替换成 Latency 的当地钟面整点。
	for at := start.Truncate(time.Hour); at.Before(end); at = at.Add(time.Hour) {
		if !at.Before(start) {
			result.keys = append(result.keys, timeutil.FormatStorageTime(at))
		}
	}
	return result
}

func (b usageBucketRead) query(query *gorm.DB) *gorm.DB {
	if b.stableOffset {
		// 普通区间保留原索引范围和旧行兼容性（包括历史非标准桶键），上海读数不变。
		return query.Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(b.start), timeutil.FormatStorageTime(b.end))
	}
	// 页面范围有界（小时至多跨 DST 日、日 <=365）；IN 使用既有 bucket_start 索引，
	// 不对时间列套函数，也不依赖含 offset 字符串的大小顺序。
	return query.Where("bucket_start IN ?", b.keys)
}

// 两侧保留两日，避免范围自身 offset 相同、却落在回拨的重复钟面附近。
// 没有转换的单一区段中，既有本地时间文本顺序与时间顺序一致。
func usageReadHasStableOffset(start, end time.Time) bool {
	transition := timeutil.ZoneSegmentEnd(timeutil.NormalizeStorageTime(start.Add(-48 * time.Hour)))
	return transition.IsZero() || transition.After(end.Add(48*time.Hour))
}

func (b usageBucketRead) displayStart(stored time.Time) time.Time {
	if start, ok := b.displayStarts[stored.Unix()]; ok {
		return start
	}
	return timeutil.NormalizeStorageTime(stored)
}

func latencyBucketRead(start, end time.Time, bucketType entities.UsageLatencyBucketType) usageBucketRead {
	if bucketType == entities.UsageLatencyBucketDay {
		// 两个数据源的旧日桶均为 time.Date(年月日, 00:00, location)。
		return overviewBucketRead(start, end, "daily")
	}
	start, end = timeutil.NormalizeStorageTime(start), timeutil.NormalizeStorageTime(end)
	result := usageBucketRead{start: start, end: end, stableOffset: usageReadHasStableOffset(start, end)}
	if result.stableOffset {
		return result
	}
	seen := make(map[string]bool)
	for cursor := start; cursor.Before(end); {
		// 与 Latency 既有写入一致，按当地钟面减去分秒。切换点可能在半小时，
		// 因此跨 timezone segment 时重新取整，不能沿首个 offset 一直加小时。
		at := cursor.Add(-time.Duration(cursor.Minute())*time.Minute - time.Duration(cursor.Second())*time.Second - time.Duration(cursor.Nanosecond()))
		key := timeutil.FormatStorageTime(at)
		if !at.Before(start) && !seen[key] {
			result.keys = append(result.keys, key)
			seen[key] = true
		}
		next := at.Add(time.Hour)
		segmentEnd := timeutil.ZoneSegmentEnd(cursor)
		if !segmentEnd.IsZero() && segmentEnd.Before(next) {
			next = segmentEnd
		}
		cursor = next
	}
	return result
}
