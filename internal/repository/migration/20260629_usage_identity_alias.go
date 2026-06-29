package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addUsageIdentityAliasMigration(tx *gorm.DB) error {
	if tx.Migrator().HasColumn(&entities.UsageIdentity{}, "alias") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, "Alias"); err != nil {
		return fmt.Errorf("add usage_identities.alias column: %w", err)
	}
	return nil
}
