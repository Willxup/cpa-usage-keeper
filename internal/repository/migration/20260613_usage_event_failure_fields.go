package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventFailureFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	columns := []struct {
		name string
		sql  string
	}{
		{name: "failure_status_code", sql: "ALTER TABLE usage_events ADD COLUMN failure_status_code INTEGER"},
		{name: "failure_code", sql: "ALTER TABLE usage_events ADD COLUMN failure_code TEXT NOT NULL DEFAULT ''"},
		{name: "failure_message", sql: "ALTER TABLE usage_events ADD COLUMN failure_message TEXT NOT NULL DEFAULT ''"},
		{name: "failure_body", sql: "ALTER TABLE usage_events ADD COLUMN failure_body TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}
	return nil
}
