package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// rebuildQuotaHistoryMigration 明确丢弃错误 Codex 历史，并创建可供其它 provider 共用的空表。
func rebuildQuotaHistoryMigration(tx *gorm.DB) error {
	// 旧子表必须先删，避免真实外键阻止删除旧父表；不存在时保持迁移可重入。
	if tx.Migrator().HasTable(&entities.CodexQuotaPercentSegment{}) {
		if err := tx.Migrator().DropTable(&entities.CodexQuotaPercentSegment{}); err != nil {
			return fmt.Errorf("drop legacy codex quota percent segments: %w", err)
		}
	}
	if tx.Migrator().HasTable(&entities.CodexQuotaCycle{}) {
		if err := tx.Migrator().DropTable(&entities.CodexQuotaCycle{}); err != nil {
			return fmt.Errorf("drop legacy codex quota cycles: %w", err)
		}
	}
	if err := tx.AutoMigrate(&entities.QuotaCycle{}, &entities.QuotaPercentSegment{}); err != nil {
		return fmt.Errorf("create generic quota history schema: %w", err)
	}
	return nil
}
