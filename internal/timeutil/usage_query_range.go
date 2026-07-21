package timeutil

import (
	"fmt"
	"strings"
	"time"
)

// UsageQueryUnit 表示 Usage 查询使用小时或自然日作为选择单位。
type UsageQueryUnit string

const (
	UsageQueryUnitHour UsageQueryUnit = "hour"
	UsageQueryUnitDay  UsageQueryUnit = "day"
)

// UsageQueryRange 是脱离 HTTP 与业务 DTO 的规范化 Usage 时间范围。
type UsageQueryRange struct {
	Range        string
	Unit         UsageQueryUnit
	Count        int
	StartTime    time.Time
	EndTime      time.Time
	EndExclusive bool
}

// ParseUsageQueryRange 统一解析 Overview、Analysis、Events 与 Activity 共用的时间参数。
func ParseUsageQueryRange(rangeValue, unitValue, startValue, endValue string, anchor time.Time) (UsageQueryRange, error) {
	rangeValue = strings.TrimSpace(rangeValue)
	if rangeValue == "" {
		return UsageQueryRange{}, fmt.Errorf("usage range is required")
	}

	switch rangeValue {
	case "today", "yesterday":
		localAnchor := NormalizeStorageTime(anchor)
		localStart := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
		if rangeValue == "yesterday" {
			localStart = localStart.AddDate(0, 0, -1)
		}
		return UsageQueryRange{
			Range:     rangeValue,
			Unit:      UsageQueryUnitDay,
			Count:     1,
			StartTime: NormalizeStorageTime(localStart),
			EndTime:   NormalizeStorageTime(localStart.AddDate(0, 0, 1).Add(-time.Nanosecond)),
		}, nil
	case "custom":
		return parseCustomUsageQueryRange(unitValue, startValue, endValue, anchor)
	default:
		rollingRange, ok := ParseUsageRollingRange(rangeValue)
		if !ok {
			return UsageQueryRange{}, fmt.Errorf("unsupported usage range %q", rangeValue)
		}
		endTime := NormalizeStorageTime(anchor)
		unit := UsageQueryUnitHour
		if rollingRange.Unit == UsageRollingUnitDay {
			unit = UsageQueryUnitDay
		}
		return UsageQueryRange{
			Range:     rangeValue,
			Unit:      unit,
			Count:     rollingRange.Value,
			StartTime: NormalizeStorageTime(endTime.Add(-rollingRange.Duration())),
			EndTime:   endTime,
		}, nil
	}
}

func parseCustomUsageQueryRange(unitValue, startValue, endValue string, anchor time.Time) (UsageQueryRange, error) {
	startValue = strings.TrimSpace(startValue)
	endValue = strings.TrimSpace(endValue)
	if startValue == "" || endValue == "" {
		return UsageQueryRange{}, fmt.Errorf("custom range requires start and end")
	}
	unit, err := resolveUsageQueryUnit(unitValue, startValue, endValue)
	if err != nil {
		return UsageQueryRange{}, err
	}
	if unit == UsageQueryUnitDay {
		return parseCustomUsageDayRange(startValue, endValue, anchor)
	}
	return parseCustomUsageHourRange(startValue, endValue, anchor)
}

func resolveUsageQueryUnit(unitValue, startValue, endValue string) (UsageQueryUnit, error) {
	unit := UsageQueryUnit(strings.TrimSpace(unitValue))
	if unit == "" {
		_, startErr := time.ParseInLocation(time.DateOnly, startValue, time.Local)
		_, endErr := time.ParseInLocation(time.DateOnly, endValue, time.Local)
		if startErr == nil && endErr == nil {
			return UsageQueryUnitDay, nil
		}
		unit = UsageQueryUnitHour
	}
	if unit != UsageQueryUnitHour && unit != UsageQueryUnitDay {
		return "", fmt.Errorf("unsupported custom range unit %q", unit)
	}
	return unit, nil
}

func parseCustomUsageDayRange(startValue, endValue string, anchor time.Time) (UsageQueryRange, error) {
	startTime, err := time.ParseInLocation(time.DateOnly, startValue, time.Local)
	if err != nil {
		return UsageQueryRange{}, fmt.Errorf("invalid start: %w", err)
	}
	endDay, err := time.ParseInLocation(time.DateOnly, endValue, time.Local)
	if err != nil {
		return UsageQueryRange{}, fmt.Errorf("invalid end: %w", err)
	}
	if startTime.After(endDay) {
		return UsageQueryRange{}, fmt.Errorf("custom range start must be before end")
	}
	if !anchor.IsZero() {
		localAnchor := NormalizeStorageTime(anchor)
		today := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
		if startTime.Before(today.AddDate(0, 0, -29)) {
			return UsageQueryRange{}, fmt.Errorf("custom day range cannot start more than 30 calendar days ago")
		}
		if endDay.After(today) {
			return UsageQueryRange{}, fmt.Errorf("custom day range cannot end in the future")
		}
	}

	count := 1
	for day := startTime; day.Before(endDay); day = day.AddDate(0, 0, 1) {
		count++
	}
	return UsageQueryRange{
		Range:        "custom",
		Unit:         UsageQueryUnitDay,
		Count:        count,
		StartTime:    NormalizeStorageTime(startTime),
		EndTime:      NormalizeStorageTime(endDay.AddDate(0, 0, 1)),
		EndExclusive: true,
	}, nil
}

func parseCustomUsageHourRange(startValue, endValue string, anchor time.Time) (UsageQueryRange, error) {
	startTime, err := parseUsageQueryHour(startValue)
	if err != nil {
		return UsageQueryRange{}, fmt.Errorf("invalid start: %w", err)
	}
	endHour, err := parseUsageQueryHour(endValue)
	if err != nil {
		return UsageQueryRange{}, fmt.Errorf("invalid end: %w", err)
	}
	if startTime.After(endHour) {
		return UsageQueryRange{}, fmt.Errorf("custom range start must be before end")
	}
	selectedHours := int(endHour.Sub(startTime)/time.Hour) + 1
	if endHour.Sub(startTime)%time.Hour != 0 || selectedHours < 5 || selectedHours > 24 {
		return UsageQueryRange{}, fmt.Errorf("custom hour range must include between 5 and 24 hours")
	}
	if !anchor.IsZero() {
		localAnchor := NormalizeStorageTime(anchor)
		currentHour := localAnchor.Add(-time.Duration(localAnchor.Minute())*time.Minute - time.Duration(localAnchor.Second())*time.Second - time.Duration(localAnchor.Nanosecond()))
		if startTime.Before(currentHour.Add(-23 * time.Hour)) {
			return UsageQueryRange{}, fmt.Errorf("custom hour range cannot start more than 24 hours ago")
		}
		if endHour.After(currentHour) {
			return UsageQueryRange{}, fmt.Errorf("custom hour range cannot end in the future")
		}
	}
	return UsageQueryRange{
		Range:        "custom",
		Unit:         UsageQueryUnitHour,
		Count:        selectedHours,
		StartTime:    startTime,
		EndTime:      NormalizeStorageTime(endHour.Add(time.Hour)),
		EndExclusive: true,
	}, nil
}

func parseUsageQueryHour(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	parsed = NormalizeStorageTime(parsed)
	if parsed.Minute() != 0 || parsed.Second() != 0 || parsed.Nanosecond() != 0 {
		return time.Time{}, fmt.Errorf("hour must align to the start of an hour")
	}
	return parsed, nil
}
