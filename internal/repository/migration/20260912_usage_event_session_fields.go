package migration

import (
	"fmt"

	"gorm.io/gorm"

	"cpa-usage-keeper/internal/entities"
)

// addUsageEventSessionFieldsMigration 手动 ALTER TABLE 添加 session_id/parent_session_id。
// 不使用 AutoMigrate：GORM 的 SQLite migrator 在为已有数据的表新增 NOT NULL 列时会
// 走"建临时表+拷贝数据+删旧表+改名"的重建路径，一旦拷贝逻辑遗漏字段就会丢数据；
// 显式 ALTER TABLE ADD COLUMN ... NOT NULL DEFAULT '' 在 SQLite 中是原地元数据变更，
// 不接触既有行,从根本上避免这一类重建导致的数据丢失。
func addUsageEventSessionFieldsMigration(tx *gorm.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{name: "session_id", sql: "ALTER TABLE usage_events ADD COLUMN session_id TEXT NOT NULL DEFAULT ''"},
		{name: "parent_session_id", sql: "ALTER TABLE usage_events ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}

	archiveColumns := []struct {
		name string
		sql  string
	}{
		{name: "session_id", sql: "ALTER TABLE usage_events_archive ADD COLUMN session_id TEXT NOT NULL DEFAULT ''"},
		{name: "parent_session_id", sql: "ALTER TABLE usage_events_archive ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range archiveColumns {
		if tx.Migrator().HasColumn(&entities.UsageEventArchive{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_events_archive.%s column: %w", column.name, err)
		}
	}
	return nil
}
