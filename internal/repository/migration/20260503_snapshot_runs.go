package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func dropSnapshotRunsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("snapshot_runs") {
		return nil
	}
	if err := tx.Exec("DROP TABLE IF EXISTS snapshot_runs").Error; err != nil {
		return fmt.Errorf("drop snapshot_runs table: %w", err)
	}
	return nil
}

func dropLegacySnapshotRunColumnsMigration(tx *gorm.DB) error {
	for _, indexName := range []string{"idx_usage_events_snapshot_run_id", "idx_redis_usage_inboxes_snapshot_run_id"} {
		if err := tx.Exec("DROP INDEX IF EXISTS " + indexName).Error; err != nil {
			return fmt.Errorf("drop legacy snapshot_run_id index %s: %w", indexName, err)
		}
	}
	if err := dropColumnIfExists(tx, &models.UsageEvent{}, "snapshot_run_id", "usage_events"); err != nil {
		return err
	}
	if err := dropColumnIfExists(tx, &models.RedisUsageInbox{}, "snapshot_run_id", "redis_usage_inboxes"); err != nil {
		return err
	}
	return nil
}
