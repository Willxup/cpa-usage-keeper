package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addModelPricePerRequestMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&entities.ModelPriceSetting{}); err != nil {
		return fmt.Errorf("auto migrate model price per request: %w", err)
	}
	return nil
}
