package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func modelPriceServiceTierMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.ModelPriceSetting{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "service_tier") {
		if err := tx.Exec("ALTER TABLE model_price_settings ADD COLUMN service_tier TEXT NOT NULL DEFAULT ''").Error; err != nil {
			return fmt.Errorf("add model_price_settings.service_tier column: %w", err)
		}
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS uniq_model_price_settings_model`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_model_price_settings_model_tier ON model_price_settings (model, service_tier)`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("rebuild model_price_settings service tier indexes: %w", err)
		}
	}
	return nil
}
