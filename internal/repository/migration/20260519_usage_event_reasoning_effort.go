package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventReasoningEffortMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if tx.Migrator().HasColumn(&entities.UsageEvent{}, "reasoning_effort") {
		return nil
	}
	if err := tx.Exec("ALTER TABLE usage_events ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT ''").Error; err != nil {
		return fmt.Errorf("add usage_events.reasoning_effort column: %w", err)
	}
	return nil
}
