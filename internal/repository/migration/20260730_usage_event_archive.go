package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createUsageEventArchiveMigration(db *gorm.DB) error {
	if err := db.AutoMigrate(&entities.UsageEventArchive{}); err != nil {
		return fmt.Errorf("create usage event archive schema: %w", err)
	}
	return nil
}
