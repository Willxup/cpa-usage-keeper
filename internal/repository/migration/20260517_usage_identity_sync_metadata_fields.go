package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageIdentitySyncMetadataFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) {
		return nil
	}

	columns := []struct {
		name string
		sql  string
	}{
		{name: "priority", sql: "ALTER TABLE usage_identities ADD COLUMN priority INTEGER"},
		{name: "disabled", sql: "ALTER TABLE usage_identities ADD COLUMN disabled NUMERIC"},
		{name: "note", sql: "ALTER TABLE usage_identities ADD COLUMN note TEXT"},
	}

	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageIdentity{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add usage_identities.%s column: %w", column.name, err)
		}
	}
	return nil
}
