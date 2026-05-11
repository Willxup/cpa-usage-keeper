package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func updateUsageIdentityQuotaFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || tx.Migrator().HasColumn(&entities.UsageIdentity{}, "project_id") {
		return nil
	}
	if err := tx.Exec("ALTER TABLE usage_identities ADD COLUMN project_id TEXT").Error; err != nil {
		return fmt.Errorf("add usage_identities.project_id column: %w", err)
	}
	return nil
}
