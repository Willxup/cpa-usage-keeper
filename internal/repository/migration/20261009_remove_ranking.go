package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// removeRankingMigration 在默认事务中精确清退废弃排名结构与设置，保留共享 Key 和其他配置。
func removeRankingMigration(tx *gorm.DB) error {
	if tx.Migrator().HasTable("local_ranking_period_stats") {
		if err := tx.Migrator().DropTable("local_ranking_period_stats"); err != nil {
			return fmt.Errorf("drop local ranking period stats: %w", err)
		}
	}
	if err := dropColumnIfExists(tx, "cpa_api_keys", "local_ranking_avatar_id", "cpa_api_keys"); err != nil {
		return err
	}
	if err := tx.Where("setting_key IN ?", []string{"ranking.identity", "ranking.local.period_state"}).Delete(&entities.AppSetting{}).Error; err != nil {
		return fmt.Errorf("delete ranking settings: %w", err)
	}
	return nil
}
