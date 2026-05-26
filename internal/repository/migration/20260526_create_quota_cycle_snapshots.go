package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func createQuotaCycleSnapshotsMigration(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&entities.QuotaCycleSnapshot{}) {
		return nil
	}
	if err := tx.Migrator().CreateTable(&entities.QuotaCycleSnapshot{}); err != nil {
		return fmt.Errorf("create quota_cycle_snapshots table: %w", err)
	}
	return nil
}
