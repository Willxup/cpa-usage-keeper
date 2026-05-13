package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageIdentityBaseURLMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		return nil
	}
	if tx.Migrator().HasColumn(&entities.UsageIdentity{}, "base_url") {
		return nil
	}
	if err := tx.Exec("ALTER TABLE usage_identities ADD COLUMN base_url TEXT").Error; err != nil {
		return fmt.Errorf("add usage_identities.base_url column: %w", err)
	}
	return nil
}
