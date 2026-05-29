package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// addUsageEventTTFTServiceTierMigration 为 usage_events 增补 CPA 新上报的
// ttft_ms（首字延迟）、service_tier（服务层级）、fail_status_code（失败状态码）三列。
func addUsageEventTTFTServiceTierMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}

	type columnDef struct {
		name string
		ddl  string
	}
	columns := []columnDef{
		{name: "ttft_ms", ddl: "ALTER TABLE usage_events ADD COLUMN ttft_ms INTEGER NOT NULL DEFAULT 0"},
		{name: "service_tier", ddl: "ALTER TABLE usage_events ADD COLUMN service_tier TEXT NOT NULL DEFAULT ''"},
		{name: "fail_status_code", ddl: "ALTER TABLE usage_events ADD COLUMN fail_status_code INTEGER NOT NULL DEFAULT 0"},
	}

	for _, column := range columns {
		if tx.Migrator().HasColumn(&entities.UsageEvent{}, column.name) {
			continue
		}
		if err := tx.Exec(column.ddl).Error; err != nil {
			return fmt.Errorf("add usage_events.%s column: %w", column.name, err)
		}
	}
	return nil
}
