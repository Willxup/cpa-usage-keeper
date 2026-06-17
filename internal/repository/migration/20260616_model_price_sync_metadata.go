package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addModelPriceSyncMetadataMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.ModelPriceSetting{}) {
		return nil
	}

	columns := []struct {
		name string
		sql  string
	}{
		{name: "source", sql: "ALTER TABLE model_price_settings ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'"},
		{name: "source_url", sql: "ALTER TABLE model_price_settings ADD COLUMN source_url TEXT"},
		{name: "synced_at", sql: "ALTER TABLE model_price_settings ADD COLUMN synced_at DATETIME"},
	}

	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, column.name) {
			continue
		}
		if err := tx.Exec(column.sql).Error; err != nil {
			return fmt.Errorf("add model_price_settings.%s column: %w", column.name, err)
		}
	}
	if tx.Migrator().HasColumn(&entities.ModelPriceSetting{}, "source") {
		if err := tx.Exec("UPDATE model_price_settings SET source = 'manual' WHERE TRIM(COALESCE(source, '')) = ''").Error; err != nil {
			return fmt.Errorf("backfill model_price_settings.source: %w", err)
		}
	}
	return nil
}
