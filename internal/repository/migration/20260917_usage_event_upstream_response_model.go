package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventUpstreamResponseModelMigration(tx *gorm.DB) error {
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
		if tx.Migrator().HasColumn(table.model, "upstream_response_model") {
			continue
		}
		if err := tx.Migrator().AddColumn(table.model, "UpstreamResponseModel"); err != nil {
			return fmt.Errorf("add %s.upstream_response_model column: %w", table.name, err)
		}
	}
	return nil
}
