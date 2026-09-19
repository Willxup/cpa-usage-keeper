package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addUsageEventStreamStatusCodeMigration(tx *gorm.DB) error {
	for _, table := range []struct {
		model any
		name  string
	}{
		{model: &entities.UsageEvent{}, name: "usage_events"},
		{model: &entities.UsageEventArchive{}, name: "usage_events_archive"},
	} {
		if !tx.Migrator().HasTable(table.model) {
			continue
		}
		for _, column := range []struct {
			name  string
			field string
		}{
			{name: "status_code", field: "StatusCode"},
			{name: "stream", field: "Stream"},
		} {
			if tx.Migrator().HasColumn(table.model, column.name) {
				continue
			}
			if err := tx.Migrator().AddColumn(table.model, column.field); err != nil {
				return fmt.Errorf("add %s.%s column: %w", table.name, column.name, err)
			}
		}
	}
	return nil
}
