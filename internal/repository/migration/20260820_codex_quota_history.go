package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// createCodexQuotaHistoryMigration 为已有数据库创建 Codex 主额度周期父表和百分比状态子表。
func createCodexQuotaHistoryMigration(tx *gorm.DB) error {
	// 父表必须先创建，子表随后才能建立真实 cycle_id 外键和 RESTRICT 删除规则。
	if err := tx.AutoMigrate(&entities.CodexQuotaCycle{}, &entities.CodexQuotaPercentSegment{}); err != nil {
		// 返回带业务表名的错误，启动日志可以直接定位 schema 初始化失败范围。
		return fmt.Errorf("create codex quota history schema: %w", err)
	}
	// AutoMigrate 已原子创建两表、CHECK、UNIQUE 和外键；默认 migration 外层事务负责版本标记。
	return nil
}
