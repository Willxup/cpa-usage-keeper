package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addAuthSessionSourceMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasColumn(&entities.AuthSession{}, "Source") {
		if err := tx.Migrator().AddColumn(&entities.AuthSession{}, "Source"); err != nil {
			return fmt.Errorf("add auth_sessions.source column: %w", err)
		}
	}
	if err := tx.Model(&entities.AuthSession{}).
		Where("source IS NULL OR TRIM(source) = ''").
		Update("source", string(auth.SessionSourceStandard)).Error; err != nil {
		return fmt.Errorf("backfill auth_sessions.source: %w", err)
	}
	return nil
}
