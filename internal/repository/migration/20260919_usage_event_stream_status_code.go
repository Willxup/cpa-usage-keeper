package migration

import (
	"fmt"
	"strings"

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
		// SQLite HasColumn matches SQL suffixes, so fail_status_code can be
		// mistaken for status_code. Inspect the actual column names instead.
		var columns []struct{ Name string }
		if err := tx.Raw("PRAGMA table_info(" + table.name + ")").Scan(&columns).Error; err != nil {
			return fmt.Errorf("inspect %s columns: %w", table.name, err)
		}
		existing := make(map[string]bool, len(columns))
		for _, column := range columns {
			existing[strings.ToLower(column.Name)] = true
		}
		for _, column := range []struct {
			name  string
			field string
		}{
			{name: "status_code", field: "StatusCode"},
			{name: "stream", field: "Stream"},
		} {
			if existing[column.name] {
				continue
			}
			if err := tx.Migrator().AddColumn(table.model, column.field); err != nil {
				return fmt.Errorf("add %s.%s column: %w", table.name, column.name, err)
			}
		}
	}
	return nil
}
