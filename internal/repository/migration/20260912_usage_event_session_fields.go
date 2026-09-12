package migration

import (
	"gorm.io/gorm"

	"cpa-usage-keeper/internal/entities"
)

func addUsageEventSessionFieldsMigration(tx *gorm.DB) error {
	return tx.AutoMigrate(&entities.UsageEvent{}, &entities.UsageEventArchive{})
}
