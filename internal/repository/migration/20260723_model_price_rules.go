package migration

import (
	"fmt"

	"gorm.io/gorm"
)

func createModelPriceRulesMigration(db *gorm.DB) error {
	if err := db.AutoMigrate(&legacyModelPriceRule{}); err != nil {
		return fmt.Errorf("create model price rules table: %w", err)
	}
	return nil
}
