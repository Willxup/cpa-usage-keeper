package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// addUsageEventAPIGroupKeyTimestampIndexMigration adds composite index on (api_group_key, timestamp DESC)
// to optimize dashboard time-window filtering and key-scoped aggregations.
func addUsageEventAPIGroupKeyTimestampIndexMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("usage_events") {
		return nil
	}
	if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_events_api_group_key_timestamp ON usage_events(api_group_key, timestamp DESC)`).Error; err != nil {
		return fmt.Errorf("create idx_usage_events_api_group_key_timestamp: %w", err)
	}
	return nil
}
