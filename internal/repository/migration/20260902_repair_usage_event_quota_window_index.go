package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

const usageEventQuotaWindowIndexName = "idx_usage_events_auth_index_timestamp_id"

// repairUsageEventQuotaWindowIndexMigration 修复旧 migration 已记录完成、物理索引却缺失的 schema 漂移。
func repairUsageEventQuotaWindowIndexMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return fmt.Errorf("repair usage event quota window index: usage_events table is missing")
	}
	if tx.Migrator().HasIndex(&entities.UsageEvent{}, usageEventQuotaWindowIndexName) {
		return nil
	}
	// 复用 UsageEvent 的复合索引声明，避免修复 migration 与当前实体列顺序发生漂移。
	if err := tx.Migrator().CreateIndex(&entities.UsageEvent{}, usageEventQuotaWindowIndexName); err != nil {
		return fmt.Errorf("repair usage event quota window index: %w", err)
	}
	return nil
}
