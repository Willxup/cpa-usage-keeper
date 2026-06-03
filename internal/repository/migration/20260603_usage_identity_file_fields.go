package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageIdentityFileFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		return nil
	}

	columns := []struct {
		field string
		name  string
	}{
		{field: "FileName", name: "file_name"},
		{field: "FilePath", name: "file_path"},
	}

	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageIdentity{}, column.name) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, column.field); err != nil {
			return fmt.Errorf("add usage_identities.%s column: %w", column.name, err)
		}
	}
	return nil
}
