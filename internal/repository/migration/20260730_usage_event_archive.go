package migration

import (
	"fmt"

	"gorm.io/gorm"
)

func createUsageEventArchiveMigration(db *gorm.DB) error {
	if err := db.AutoMigrate(&legacyUsageEventArchive{}); err != nil {
		return fmt.Errorf("create usage event archive schema: %w", err)
	}
	return nil
}
