package timeutil

import "time"

// CivilDate 只承载年月日，不把跳时后的异常午夜带到下一日。
type CivilDate struct {
	Year  int
	Month time.Month
	Day   int
}

func DateOf(value time.Time) CivilDate {
	year, month, day := value.Date()
	return CivilDate{Year: year, Month: month, Day: day}
}

func ParseCivilDate(value string) (CivilDate, error) {
	date, err := time.Parse(time.DateOnly, value)
	return DateOf(date), err
}

func (date CivilDate) calendarTime() time.Time {
	return time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC)
}

func (date CivilDate) AddDays(days int) CivilDate {
	return DateOf(date.calendarTime().AddDate(0, 0, days))
}

func (date CivilDate) String() string { return date.calendarTime().Format(time.DateOnly) }

// DaysUntil 使用历法载体而不是当地日长；Unix 秒差也避免远日期的 Duration 溢出。
func (date CivilDate) DaysUntil(other CivilDate) int {
	return int((other.calendarTime().Unix() - date.calendarTime().Unix()) / 86400)
}

// Interval 是查询使用的真实半开区间，不定义存储桶或事件归属。
type Interval struct {
	Start time.Time
	End   time.Time
}

func (interval Interval) Minutes() float64 { return interval.End.Sub(interval.Start).Minutes() }

func ElapsedRange(anchor time.Time, duration time.Duration) Interval {
	return Interval{Start: anchor.Add(-duration), End: anchor}
}

// ZoneSegmentEnd 保留 ZoneBounds 的真实转换边界，并修正 Go POSIX TZ
// 延伸规则将闰年末尾按 365 天计算的边界。该分支已在当年最后一次转换后，
// 因此只补齐到下一 UTC 年起点，再由下一次查询读取后续规则。
func ZoneSegmentEnd(at time.Time) time.Time {
	_, end := at.ZoneBounds()
	if end.IsZero() || end.After(at) {
		return end
	}
	year := at.UTC().Year()
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	next := start.AddDate(1, 0, 0)
	shortEnd := start.Add(365 * 24 * time.Hour)
	if shortEnd.Before(next) && end.Equal(shortEnd) {
		return next.In(at.Location())
	}
	return end
}

// DayBounds 按时区转换区段求某日期的真实范围，避免 time.Date 对不存在/重复午夜的歧义。
// UTC 仅作年月日坐标；每段按当时的实际 offset 映射，结果仍返回调用方时区。
// IANA 日期变更最多跨一个整日；两侧各留两日覆盖其 offset 和整日跳过，不逐秒搜索。
func DayBounds(date CivilDate, location *time.Location) (Interval, bool) {
	nominalStart := date.calendarTime()
	nominalEnd := nominalStart.AddDate(0, 0, 1)
	searchEnd := nominalEnd.Add(48 * time.Hour)
	var result Interval
	found := false
	for cursor := nominalStart.Add(-48 * time.Hour); cursor.Before(searchEnd); {
		local := cursor.In(location)
		_, offset := local.Zone()
		segmentEnd := ZoneSegmentEnd(local)
		if segmentEnd.IsZero() || segmentEnd.After(searchEnd) {
			segmentEnd = searchEnd
		}
		start := nominalStart.Add(-time.Duration(offset) * time.Second)
		end := nominalEnd.Add(-time.Duration(offset) * time.Second)
		if start.Before(cursor) {
			start = cursor
		}
		if end.After(segmentEnd) {
			end = segmentEnd
		}
		if start.Before(end) {
			if !found {
				result.Start = start.In(location)
				found = true
			}
			result.End = end.In(location)
		}
		cursor = segmentEnd
	}
	return result, found
}
