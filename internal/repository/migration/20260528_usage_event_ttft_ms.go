package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventTTFTMSMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if tx.Migrator().HasColumn(&entities.UsageEvent{}, "ttft_ms") {
		return nil
	}
	if err := tx.Exec("ALTER TABLE usage_events ADD COLUMN ttft_ms INTEGER").Error; err != nil {
		return fmt.Errorf("add usage_events.ttft_ms column: %w", err)
	}
	return nil
}
