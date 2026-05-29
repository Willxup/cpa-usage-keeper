package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventServiceTierMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if tx.Migrator().HasColumn(&entities.UsageEvent{}, "service_tier") {
		return nil
	}
	if err := tx.Exec("ALTER TABLE usage_events ADD COLUMN service_tier TEXT NOT NULL DEFAULT ''").Error; err != nil {
		return fmt.Errorf("add usage_events.service_tier column: %w", err)
	}
	return nil
}
