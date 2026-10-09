package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// normalizeUsageEventParentSessionNullMigration 统一父会话缺失值，避免根会话同时使用 NULL 与空字符串。
func normalizeUsageEventParentSessionNullMigration(tx *gorm.DB) error {
	for _, table := range []struct {
		model any
		name  string
	}{
		{model: &entities.UsageEvent{}, name: "usage_events"},
		{model: &entities.UsageEventArchive{}, name: "usage_events_archive"},
	} {
		if !tx.Migrator().HasTable(table.model) || !tx.Migrator().HasColumn(table.model, "parent_session_id") {
			continue
		}
		if err := tx.Table(table.name).
			Where("parent_session_id = ?", "").
			UpdateColumn("parent_session_id", nil).Error; err != nil {
			return fmt.Errorf("normalize %s.parent_session_id: %w", table.name, err)
		}
	}
	return nil
}
