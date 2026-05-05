package migration

import (
	"fmt"

	"gorm.io/gorm"
)

func dropLegacyMetadataTablesMigration(tx *gorm.DB) error {
	if err := tx.Exec("DROP TABLE IF EXISTS auth_files").Error; err != nil {
		return fmt.Errorf("drop auth_files table: %w", err)
	}
	if err := tx.Exec("DROP TABLE IF EXISTS provider_metadata").Error; err != nil {
		return fmt.Errorf("drop provider_metadata table: %w", err)
	}
	return nil
}
