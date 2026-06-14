package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createAuthFileCooldownsTableMigration(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&entities.AuthFileCooldown{}) {
		return nil
	}
	if err := tx.AutoMigrate(&entities.AuthFileCooldown{}); err != nil {
		return fmt.Errorf("create auth_file_cooldowns table: %w", err)
	}
	return nil
}
