package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func addUsageEventResponseServiceTierMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&entities.UsageEvent{}, "response_service_tier") {
		if err := tx.Migrator().AddColumn(&entities.UsageEvent{}, "ResponseServiceTier"); err != nil {
			return fmt.Errorf("add usage_events.response_service_tier column: %w", err)
		}
	}

	// 新列加入旧库后，对所有历史事件回填响应 tier；请求侧 auto 统一按 default 迁移。
	if err := tx.Model(&entities.UsageEvent{}).
		Where("response_service_tier = ?", "").
		Update("response_service_tier", gorm.Expr(
			"CASE WHEN LOWER(TRIM(service_tier)) = ? THEN ? ELSE service_tier END",
			"auto",
			"default",
		)).Error; err != nil {
		return fmt.Errorf("backfill usage_events.response_service_tier: %w", err)
	}
	return nil
}
