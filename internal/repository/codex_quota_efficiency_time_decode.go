package repository

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"cpa-usage-keeper/internal/timeutil"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm/schema"
)

// 这两个私有读取类型只用于额度历史的 DATETIME 投影，不改变实体或全局 serializer。
type codexQuotaEfficiencyEventTime struct{ time.Time }

func (at *codexQuotaEfficiencyEventTime) Scan(value any) error {
	decoded, err := scanCodexQuotaEfficiencyEventTime(value)
	if err == nil {
		at.Time = decoded
	}
	return err
}

// 由字段自身实现 GORM serializer，保留原来逐字段 Set 的错误顺序和聚合行为。
type codexQuotaEfficiencyObservedTime struct{ time.Time }

func (at *codexQuotaEfficiencyObservedTime) Scan(_ context.Context, _ *schema.Field, _ reflect.Value, value any) error {
	decoded, err := scanCodexQuotaEfficiencyObservedTime(value)
	if err == nil {
		at.Time = decoded
	}
	return err
}

func (at codexQuotaEfficiencyObservedTime) Value(ctx context.Context, field *schema.Field, dst reflect.Value, _ any) (any, error) {
	return (timeutil.SortableTimeSerializer{}).Value(ctx, field, dst, at.Time)
}

// 事件原本直接 Scan 到 *time.Time；NULL、BLOB 和 REAL 必须继续返回同样的类型错误。
func scanCodexQuotaEfficiencyEventTime(value any) (time.Time, error) {
	switch stored := value.(type) {
	case string:
		return parseCodexQuotaEfficiencyDatetime(stored), nil
	case int64:
		return codexQuotaEfficiencyIntegerTime(stored), nil
	case time.Time:
		return stored, nil
	default:
		return time.Time{}, fmt.Errorf("unsupported Scan, storing driver.Value type %T into type *time.Time", value)
	}
}

// 子段原本经过 sortableTime：NULL 不归一化，DATETIME 零值仍归一化，BLOB 走原字符串解析。
func scanCodexQuotaEfficiencyObservedTime(value any) (time.Time, error) {
	switch stored := value.(type) {
	case nil:
		return time.Time{}, nil
	case string:
		return timeutil.NormalizeStorageTime(parseCodexQuotaEfficiencyDatetime(stored)), nil
	case int64:
		return timeutil.NormalizeStorageTime(codexQuotaEfficiencyIntegerTime(stored)), nil
	case time.Time:
		return timeutil.NormalizeStorageTime(stored), nil
	case []byte:
		parsed, err := timeutil.ParseStorageTime(string(stored))
		if err != nil {
			return time.Time{}, err
		}
		return timeutil.NormalizeStorageTime(parsed), nil
	default:
		return time.Time{}, fmt.Errorf("scan storage time from %T", value)
	}
}

func codexQuotaEfficiencyIntegerTime(value int64) time.Time {
	// 与固定版本 go-sqlite3 保持相同严格阈值，以及 int64 毫秒乘法的溢出语义。
	if value > 1e12 || value < -1e12 {
		value *= int64(time.Millisecond)
		return time.Unix(0, value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func parseCodexQuotaEfficiencyDatetime(value string) time.Time {
	// 只给严格形状的持久 RFC3339 时间启用 Go 的快速解析；其它输入逐值复现驱动。
	if isCodexQuotaEfficiencyRFC3339(value) {
		if at, err := time.ParseInLocation(time.RFC3339Nano, value, time.UTC); err == nil {
			return at
		}
	}
	// go-sqlite3 只删掉一个尾随 Z，不 TrimSpace；无 offset 按 UTC，非法 TEXT 为零值。
	value = strings.TrimSuffix(value, "Z")
	for _, layout := range sqlite3.SQLiteTimestampFormats {
		if at, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return at
		}
	}
	return time.Time{}
}

func isCodexQuotaEfficiencyRFC3339(value string) bool {
	if len(value) < 20 || value[4] != '-' || value[7] != '-' || value[10] != 'T' || value[13] != ':' || value[16] != ':' {
		return false
	}
	for index := 0; index < 19; index++ {
		switch index {
		case 4, 7, 10, 13, 16:
			continue
		}
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	index := 19
	if value[index] == '.' {
		index++
		start := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if digits := index - start; digits < 1 || digits > 9 {
			return false
		}
	}
	if index == len(value)-1 && value[index] == 'Z' {
		return true
	}
	return len(value)-index == 6 && (value[index] == '+' || value[index] == '-') && value[index+3] == ':' &&
		value[index+1] >= '0' && value[index+1] <= '9' && value[index+2] >= '0' && value[index+2] <= '9' &&
		value[index+4] >= '0' && value[index+4] <= '9' && value[index+5] >= '0' && value[index+5] <= '9'
}
