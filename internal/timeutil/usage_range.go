package timeutil

import (
	"strconv"
	"time"
)

// UsageRollingUnit 表示按小时或按天回看的滚动查询单位。
type UsageRollingUnit byte

const (
	UsageRollingUnitHour UsageRollingUnit = 'h'
	UsageRollingUnitDay  UsageRollingUnit = 'd'
)

// UsageRollingRange 是已经通过边界校验的滚动查询范围。
type UsageRollingRange struct {
	Value int
	Unit  UsageRollingUnit
}

// Duration 返回滚动范围对应的固定时长；天范围与旧 7d/30d 语义一致，按 24 小时计算。
func (r UsageRollingRange) Duration() time.Duration {
	if r.Unit == UsageRollingUnitDay {
		return time.Duration(r.Value) * 24 * time.Hour
	}
	return time.Duration(r.Value) * time.Hour
}

// ParseUsageRollingRange 接受规范的 5-24h 或 1-30d，不接受越界值、前导零和其他单位。
func ParseUsageRollingRange(value string) (UsageRollingRange, bool) {
	if len(value) < 2 {
		return UsageRollingRange{}, false
	}
	unit := UsageRollingUnit(value[len(value)-1])
	if unit != UsageRollingUnitHour && unit != UsageRollingUnitDay {
		return UsageRollingRange{}, false
	}
	numberText := value[:len(value)-1]
	number, err := strconv.Atoi(numberText)
	if err != nil || strconv.Itoa(number) != numberText {
		return UsageRollingRange{}, false
	}
	minimum := 5
	maximum := 24
	if unit == UsageRollingUnitDay {
		minimum = 1
		maximum = 30
	}
	if number < minimum || number > maximum {
		return UsageRollingRange{}, false
	}
	return UsageRollingRange{Value: number, Unit: unit}, true
}

func IsUsageRollingHourRange(value string) bool {
	rangeValue, ok := ParseUsageRollingRange(value)
	return ok && rangeValue.Unit == UsageRollingUnitHour
}

func IsUsageRollingDayRange(value string) bool {
	rangeValue, ok := ParseUsageRollingRange(value)
	return ok && rangeValue.Unit == UsageRollingUnitDay
}
