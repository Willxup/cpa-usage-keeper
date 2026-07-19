package migration

import (
	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createAPIKeyAuthFileScopesMigration(tx *gorm.DB) error {
	return tx.AutoMigrate(&entities.APIKeyAuthFileScope{})
}
