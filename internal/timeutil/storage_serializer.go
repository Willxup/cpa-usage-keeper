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
	// storageTime 保持项目既有的本地时区存储语义。
	schema.RegisterSerializer("storageTime", StorageTimeSerializer{})
	// sortableTime 只供必须通过 SQLite TEXT 比较 instant 顺序的列使用。
	schema.RegisterSerializer("sortableTime", SortableTimeSerializer{})
}

type StorageTimeSerializer struct{}

// SortableTimeSerializer 写入固定宽度 UTC 文本，读取时仍恢复为项目时区 time.Time。
type SortableTimeSerializer struct{}

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

// Scan 复用 storageTime 的兼容解析，并把读出的 instant 归一化到项目时区。
func (SortableTimeSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	// 两种 serializer 只区分写入格式，读取都必须兼容既有 RFC3339 表达。
	return (StorageTimeSerializer{}).Scan(ctx, field, dst, dbValue)
}

// Value 把 Activity 边界写成可直接按 TEXT 比较的固定宽度 UTC 表达。
func (SortableTimeSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	// nil 边界交给数据库 not-null 约束处理。
	if fieldValue == nil {
		return nil, nil
	}
	// GORM 可能传入值或指针，两种形式都统一调用可排序格式化函数。
	switch value := fieldValue.(type) {
	case time.Time:
		// 零时间保持现有 serializer 的 NULL 语义。
		if value.IsZero() {
			return nil, nil
		}
		// 非零值始终转换为固定宽度 UTC 文本。
		return FormatSortableStorageTime(value), nil
	case *time.Time:
		// nil 或零时间指针同样写为 NULL。
		if value == nil || value.IsZero() {
			return nil, nil
		}
		// 指针值也使用完全相同的 UTC 文本格式。
		return FormatSortableStorageTime(*value), nil
	case driver.Valuer:
		// 自定义数据库值继续遵守其自身 Valuer 契约。
		return value.Value()
	default:
		// 非时间类型保持 GORM 默认透传行为。
		return fieldValue, nil
	}
}
