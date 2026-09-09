package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// resetQuotaHistoryMigration 只清空无法验证来源的旧历史；父子 schema 与其它业务数据保持不变。
func resetQuotaHistoryMigration(tx *gorm.DB) error {
	// 外键是 RESTRICT，必须先清子表；AllowGlobalUpdate 明确表达本版本有意执行全表 DELETE。
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&entities.QuotaPercentSegment{}).Error; err != nil {
		return fmt.Errorf("clear quota percent segments: %w", err)
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&entities.QuotaCycle{}).Error; err != nil {
		return fmt.Errorf("clear quota cycles: %w", err)
	}
	return nil
}
