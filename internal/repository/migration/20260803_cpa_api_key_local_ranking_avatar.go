package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// addCPAAPIKeyLocalRankingAvatarMigration 只增加可空覆盖值；NULL 继续使用 API Key ID 的默认头像映射。
func addCPAAPIKeyLocalRankingAvatarMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	if tx.Migrator().HasColumn(&entities.CPAAPIKey{}, "LocalRankingAvatarID") {
		return nil
	}
	if err := tx.Migrator().AddColumn(&entities.CPAAPIKey{}, "LocalRankingAvatarID"); err != nil {
		return fmt.Errorf("add CPA API key local ranking avatar: %w", err)
	}
	return nil
}
