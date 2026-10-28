package repository

import (
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// usageEventTimeRead 只描述既有 timestamp 文本的索引候选范围和真实时间边界。
// 投影、Key 条件、费用校验和缓存生命周期仍由调用方负责。
type usageEventTimeRead struct {
	start, end                          time.Time
	lower, upper, suffix                string
	includeEnd, openEnd, upperInclusive bool
}

func usageEventTimeReads(start, end time.Time, includeEnd bool) []usageEventTimeRead {
	if end.Before(start) || (!includeEnd && end.Equal(start)) {
		return nil
	}
	start, end = timeutil.NormalizeStorageTime(start), timeutil.NormalizeStorageTime(end)
	separateOffsets := !usageReadHasStableOffset(start, end)
	var reads []usageEventTimeRead
	for cursor := start; cursor.Before(end) || (includeEnd && cursor.Equal(end)); {
		_, offset := cursor.Zone()
		zoneEnd := timeutil.ZoneSegmentEnd(cursor)
		segmentEnd, closed := end, includeEnd
		if !zoneEnd.IsZero() && !zoneEnd.After(end) {
			segmentEnd, closed = zoneEnd, false
		}
		zone := time.FixedZone("", offset)
		read := usageEventTimeRead{start: cursor, end: segmentEnd, includeEnd: closed, upperInclusive: closed,
			lower: cursor.In(zone).Format(time.RFC3339Nano), upper: segmentEnd.In(zone).Format(time.RFC3339Nano)}
		if offset == 0 {
			// Z 排在小数点之后：以整秒前缀覆盖边界秒，再按真实时间裁剪。
			const secondPrefix = "2006-01-02T15:04:05"
			read.lower = cursor.In(zone).Format(secondPrefix)
			read.upper = segmentEnd.In(zone).Truncate(time.Second).Add(time.Second).Format(secondPrefix)
			read.upperInclusive = false
		}
		if separateOffsets {
			read.suffix = cursor.In(zone).Format("Z07:00")
		}
		reads = append(reads, read)
		if closed || segmentEnd.Equal(end) && !includeEnd {
			break
		}
		cursor = segmentEnd
	}
	return reads
}

// usageEventTimeReadSince 保留缓存启动/重载的开放上界。取已知窗口各段最小文本
// 下界覆盖回拨后的钟面；不收紧原下界，因此原本可加载的未来事件仍能进入缓存。
// 候选中额外的过期事件由 contains 按真实时间排除，不扩大缓存保留窗口。
func usageEventTimeReadSince(start, anchor time.Time) usageEventTimeRead {
	start = timeutil.NormalizeStorageTime(start)
	read := usageEventTimeRead{start: start, openEnd: true, lower: timeutil.FormatStorageTime(start)}
	for _, segment := range usageEventTimeReads(start, anchor, true) {
		if segment.lower < read.lower {
			read.lower = segment.lower
		}
	}
	return read
}

func (r usageEventTimeRead) query(query *gorm.DB) *gorm.DB {
	query = query.Where("timestamp >= ?", r.lower)
	if !r.openEnd {
		if r.upperInclusive {
			query = query.Where("timestamp <= ?", r.upper)
		} else {
			query = query.Where("timestamp < ?", r.upper)
		}
	}
	if r.suffix != "" {
		query = query.Where("timestamp LIKE ?", "%"+r.suffix)
	}
	return query
}

func (r usageEventTimeRead) contains(at time.Time) bool {
	return !at.Before(r.start) && (r.openEnd || at.Before(r.end) || (r.includeEnd && at.Equal(r.end)))
}
