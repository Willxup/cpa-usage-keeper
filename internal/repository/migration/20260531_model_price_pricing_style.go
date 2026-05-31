package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addModelPricePricingStyleMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.ModelPriceSetting{}) {
		return nil
	}
	columns := []struct {
		name string
		sql  string
	}{
		{name: "pricing_style", sql: "ALTER TABLE model_price_settings ADD COLUMN pricing_style TEXT NOT NULL DEFAULT 'openai'"},
		{name: "cache_creation_price_per1_m", sql: "ALTER TABLE model_price_settings ADD COLUMN cache_creation_price_per1_m REAL NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add model_price_settings.%s column: %w", column.name, err)
		}
	}
	return nil
}
