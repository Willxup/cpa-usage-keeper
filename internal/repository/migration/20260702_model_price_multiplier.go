package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addModelPriceMultiplierMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.ModelPriceSetting{}) {
		return nil
	}
	if tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "price_multiplier") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.ModelPriceSetting{}, "PriceMultiplier"); err != nil {
		return fmt.Errorf("add model_price_settings.price_multiplier column: %w", err)
	}
	return nil
}
