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
	if !tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "price_multiplier") {
		if err := tx.Migrator().AddColumn(&entities.ModelPriceSetting{}, "PriceMultiplier"); err != nil {
			return fmt.Errorf("add model_price_settings.price_multiplier column: %w", err)
		}
	}
	if err := tx.Model(&entities.ModelPriceSetting{}).
		Where("price_multiplier IS NULL").
		Update("price_multiplier", 1.0).Error; err != nil {
		return fmt.Errorf("backfill model_price_settings.price_multiplier: %w", err)
	}
	return nil
}
