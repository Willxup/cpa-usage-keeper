package migration

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

type storageTimeColumn struct {
	table string
	field string
}

func normalizeStorageTimesToProjectTZMigration(tx *gorm.DB) error {
	// 最后一段迁移统一收口所有历史时间字段，前置迁移保留原顺序和原逻辑。
	columns := []storageTimeColumn{
		{table: "usage_events", field: "timestamp"},
		{table: "usage_events", field: "created_at"},
		{table: "redis_usage_inboxes", field: "popped_at"},
		{table: "redis_usage_inboxes", field: "processed_at"},
		{table: "redis_usage_inboxes", field: "created_at"},
		{table: "redis_usage_inboxes", field: "updated_at"},
		{table: "usage_identities", field: "active_start"},
		{table: "usage_identities", field: "active_until"},
		{table: "usage_identities", field: "first_used_at"},
		{table: "usage_identities", field: "last_used_at"},
		{table: "usage_identities", field: "stats_updated_at"},
		{table: "usage_identities", field: "created_at"},
		{table: "usage_identities", field: "updated_at"},
		{table: "usage_identities", field: "deleted_at"},
		{table: "model_price_settings", field: "created_at"},
		{table: "model_price_settings", field: "updated_at"},
		{table: "schema_migrations", field: "applied_at"},
	}
	for _, column := range columns {
		if err := normalizeStorageTimeColumn(tx, column); err != nil {
			return err
		}
	}
	return nil
}

func storageTimeColumnExists(tx *gorm.DB, column storageTimeColumn) (bool, error) {
	// 旧库可能缺少较新的列，迁移按实际 schema 跳过不存在的字段。
	var rows []struct {
		Name string
	}
	if err := tx.Raw(fmt.Sprintf("PRAGMA table_info(%s)", column.table)).Scan(&rows).Error; err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", column.table, err)
	}
	for _, row := range rows {
		if row.Name == column.field {
			return true, nil
		}
	}
	return false, nil
}

func normalizeStorageTimeColumn(tx *gorm.DB, column storageTimeColumn) error {
	exists, err := storageTimeColumnExists(tx, column)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var rows []struct {
		ID    int64
		Value *string
	}
	// CAST 成 TEXT，避免 SQLite driver 把无时区 DATETIME 提前按 UTC 转成 time.Time。
	if err := tx.Raw(fmt.Sprintf("SELECT rowid AS id, CAST(%s AS TEXT) AS value FROM %s WHERE %s IS NOT NULL", column.field, column.table, column.field)).Scan(&rows).Error; err != nil {
		return fmt.Errorf("list %s.%s storage times: %w", column.table, column.field, err)
	}
	for _, row := range rows {
		if row.Value == nil || strings.TrimSpace(*row.Value) == "" {
			continue
		}
		parsed, err := timeutil.ParseStorageTime(*row.Value)
		if err != nil {
			return fmt.Errorf("parse %s.%s row %v value %q: %w", column.table, column.field, row.ID, *row.Value, err)
		}
		if err := tx.Exec(fmt.Sprintf("UPDATE %s SET %s = ? WHERE rowid = ?", column.table, column.field), timeutil.FormatStorageTime(parsed), row.ID).Error; err != nil {
			return fmt.Errorf("update %s.%s row %v: %w", column.table, column.field, row.ID, err)
		}
	}
	return nil
}
