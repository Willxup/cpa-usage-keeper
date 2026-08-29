package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createModelPriceRulesMigration(db *gorm.DB) error {
	if err := db.AutoMigrate(&entities.ModelPriceRule{}); err != nil {
		return fmt.Errorf("create model price rules table: %w", err)
	}
	return nil
}
