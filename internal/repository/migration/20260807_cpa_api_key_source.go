package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addCPAAPIKeySourceMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	if !tx.Migrator().HasColumn(&entities.CPAAPIKey{}, "Source") {
		if err := tx.Migrator().AddColumn(&entities.CPAAPIKey{}, "Source"); err != nil {
			return fmt.Errorf("add CPA API key source: %w", err)
		}
	}
	if err := tx.Model(&entities.CPAAPIKey{}).
		Where("source IS NULL OR TRIM(source) = ''").
		Update("source", entities.CPAAPIKeySourceNative).Error; err != nil {
		return fmt.Errorf("backfill CPA API key source: %w", err)
	}
	if !tx.Migrator().HasIndex(&entities.CPAAPIKey{}, "idx_cpa_api_keys_source") {
		if err := tx.Migrator().CreateIndex(&entities.CPAAPIKey{}, "Source"); err != nil {
			return fmt.Errorf("create CPA API key source index: %w", err)
		}
	}
	return nil
}
