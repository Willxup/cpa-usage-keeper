package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func createAuthSessionsMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&entities.AuthSession{}); err != nil {
		return fmt.Errorf("auto migrate auth sessions: %w", err)
	}
	return nil
}
