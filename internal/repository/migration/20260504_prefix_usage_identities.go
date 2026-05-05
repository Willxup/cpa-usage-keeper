package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func removePrefixUsageIdentitiesMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageIdentity{}) {
		return nil
	}
	if err := tx.Exec(`
		DELETE FROM usage_identities
		WHERE auth_type = ?
			AND LOWER(TRIM(identity)) IN ('gemini', 'claude', 'codex', 'vertex', 'openai')`, models.UsageIdentityAuthTypeAIProvider).Error; err != nil {
		return fmt.Errorf("remove prefix-generated usage identities: %w", err)
	}
	return nil
}
