package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// localRankingStatsMigration 只创建按日/月保留的轻量本地排行周期表。
func localRankingStatsMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	if err := tx.AutoMigrate(&entities.LocalRankingPeriodStat{}); err != nil {
		return fmt.Errorf("create local ranking storage: %w", err)
	}
	return nil
}
