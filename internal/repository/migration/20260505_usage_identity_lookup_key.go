package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func addUsageIdentityLookupKeyMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&models.UsageIdentity{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&models.UsageIdentity{}, "lookup_key") {
		if err := tx.Exec("ALTER TABLE usage_identities ADD COLUMN lookup_key TEXT").Error; err != nil {
			return fmt.Errorf("add usage_identities.lookup_key column: %w", err)
		}
	}
	if err := tx.Exec(`
		UPDATE usage_identities
		SET lookup_key = TRIM(identity)
		WHERE auth_type_name = 'apikey'
			AND TRIM(COALESCE(lookup_key, '')) = ''
			AND TRIM(COALESCE(identity, '')) != ''`).Error; err != nil {
		return fmt.Errorf("backfill AI provider usage identity lookup keys: %w", err)
	}
	return normalizeUsageIdentitiesSchemaMigration(tx)
}
