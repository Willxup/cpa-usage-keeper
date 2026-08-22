package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// addCPAAPIKeySourceMigration 只增加来源列并把存量行回填为 cpa；插件行由后续同步写入，不在迁移中创建。
func addCPAAPIKeySourceMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	if !tx.Migrator().HasColumn(&entities.CPAAPIKey{}, "Source") {
		if err := tx.Migrator().AddColumn(&entities.CPAAPIKey{}, "Source"); err != nil {
			return fmt.Errorf("add cpa_api_keys.source column: %w", err)
		}
	}
	// 列已存在但历史行为 NULL 或空串时统一回填为 cpa，保证后续同步只按 source 隔离。
	if err := tx.Model(&entities.CPAAPIKey{}).
		Where("source IS NULL OR TRIM(source) = ''").
		Update("source", entities.CPAAPIKeySourceCPA).Error; err != nil {
		return fmt.Errorf("backfill cpa_api_keys.source: %w", err)
	}
	return nil
}
