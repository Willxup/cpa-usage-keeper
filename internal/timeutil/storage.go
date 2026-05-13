package timeutil

import (
	"fmt"
	"strings"
	"time"
)

func NormalizeStorageTime(t time.Time) time.Time {
	return t.In(time.Local)
}

func FormatStorageTime(t time.Time) string {
	return NormalizeStorageTime(t).Format(time.RFC3339Nano)
}

// 旧库带 offset 的值按原 offset 解析；不带 offset 的值按项目 TZ 本地时间解析。
func ParseStorageTime(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("storage time is empty")
	}

	// 先处理带明确时区的格式，保留原始 instant 后再交给格式化阶段转项目 TZ。
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
	} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return parsed, nil
		}
	}

	// 无时区旧值代表项目本地时间，不能当 UTC 迁移。
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		parsed, err := time.ParseInLocation(layout, trimmed, time.Local)
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("parse storage time %q", value)
}
