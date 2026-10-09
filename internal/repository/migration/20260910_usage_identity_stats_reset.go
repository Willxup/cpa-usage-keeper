package migration

import (
	"cpa-usage-keeper/internal/entities"
	"fmt"
	"gorm.io/gorm"
)

func addUsageIdentityStatsResetMigration(tx *gorm.DB) error {
	// 旧行基线为零，升级后继续展示原累计，不回扫 usage_events。
	for _, field := range []string{"StatsResetAt", "ResetTotalRequests", "ResetSuccessCount", "ResetFailureCount", "ResetInputTokens", "ResetCacheReadTokens", "ResetTotalTokens"} {
		if tx.Migrator().HasColumn(&entities.UsageIdentity{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&entities.UsageIdentity{}, field); err != nil {
			return fmt.Errorf("add usage identity stats reset field %s: %w", field, err)
		}
	}
	return nil
}
