package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func removeUsageIdentityQuotaFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		return nil
	}

	columns := []string{
		"limit_reached",
		"primary_window_used_percent",
		"primary_window_limit_seconds",
		"primary_window_reset_seconds",
		"primary_window_reset_at",
		"secondary_window_used_percent",
		"secondary_window_limit_seconds",
		"secondary_window_reset_seconds",
		"secondary_window_reset_at",
	}
	for _, column := range columns {
		if !tx.Migrator().HasColumn(&entities.UsageIdentity{}, column) {
			continue
		}
		if err := tx.Exec(fmt.Sprintf("ALTER TABLE usage_identities DROP COLUMN %s", column)).Error; err != nil {
			return fmt.Errorf("drop usage_identities.%s column: %w", column, err)
		}
	}
	return nil
}
