package timeutil

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm/schema"
)

func init() {
	schema.RegisterSerializer("storageTime", StorageTimeSerializer{})
}

type StorageTimeSerializer struct{}

// GORM 读库时统一把历史格式和新格式还原成项目 TZ 下的 time.Time。
func (StorageTimeSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	if dbValue == nil {
		return field.Set(ctx, dst, nil)
	}
	var raw string
	switch value := dbValue.(type) {
	case time.Time:
		return field.Set(ctx, dst, NormalizeStorageTime(value))
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("scan storage time from %T", dbValue)
	}
	parsed, err := ParseStorageTime(raw)
	if err != nil {
		return err
	}
	return field.Set(ctx, dst, NormalizeStorageTime(parsed))
}

// GORM 写库时统一输出 RFC3339Nano + 项目 TZ offset，避免 SQLite TEXT 混格式比较。
func (StorageTimeSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue any) (any, error) {
	if fieldValue == nil {
		return nil, nil
	}
	switch value := fieldValue.(type) {
	case time.Time:
		if value.IsZero() {
			return nil, nil
		}
		return FormatStorageTime(value), nil
	case *time.Time:
		if value == nil || value.IsZero() {
			return nil, nil
		}
		return FormatStorageTime(*value), nil
	case driver.Valuer:
		return value.Value()
	default:
		return fieldValue, nil
	}
}
