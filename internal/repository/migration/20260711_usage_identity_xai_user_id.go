package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageIdentityXAIUserIDMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || tx.Migrator().HasColumn(&entities.UsageIdentity{}, "xai_user_id") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, "XAIUserID"); err != nil {
		return fmt.Errorf("add usage_identities.xai_user_id column: %w", err)
	}
	return nil
}
