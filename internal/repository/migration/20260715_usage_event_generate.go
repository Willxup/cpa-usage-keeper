package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventGenerateMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&entities.UsageEvent{}, "generate") {
		if err := tx.Migrator().AddColumn(&entities.UsageEvent{}, "Generate"); err != nil {
			return fmt.Errorf("add usage_events.generate column: %w", err)
		}
	}

	// 旧版 CPA 未上报 generate；仅把成功的 WebSocket 全零 token 事件回填为预热。
	if err := tx.Model(&entities.UsageEvent{}).
		Where("failed = ?", false).
		Where("executor_type = ?", "CodexWebsocketsExecutor").
		Where("input_tokens = ?", 0).
		Where("output_tokens = ?", 0).
		Where("reasoning_tokens = ?", 0).
		Where("cached_tokens = ?", 0).
		Where("cache_read_tokens = ?", 0).
		Where("cache_creation_tokens = ?", 0).
		Where("total_tokens = ?", 0).
		Update("generate", false).Error; err != nil {
		return fmt.Errorf("backfill usage_events.generate: %w", err)
	}
	return nil
}
