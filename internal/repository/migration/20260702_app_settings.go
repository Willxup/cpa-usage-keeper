package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func createAppSettingsMigration(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&entities.AppSetting{}) {
		return nil
	}
	if err := tx.Migrator().CreateTable(&entities.AppSetting{}); err != nil {
		return fmt.Errorf("create app_settings table: %w", err)
	}
	return nil
}
