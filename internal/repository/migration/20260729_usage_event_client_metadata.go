package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventClientMetadataMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	columns := []struct {
		name  string
		field string
	}{
		{name: "client_ip", field: "ClientIP"},
		{name: "x_forwarded_for", field: "XForwardedFor"},
		{name: "user_agent", field: "UserAgent"},
	}
	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.UsageEvent{}, column.field); err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}
	return nil
}
