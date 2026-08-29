package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addAuthSessionAliasMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.AuthSession{}) || tx.Migrator().HasColumn(&entities.AuthSession{}, "Alias") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.AuthSession{}, "Alias"); err != nil {
		return fmt.Errorf("add auth_sessions.alias column: %w", err)
	}
	return nil
}
