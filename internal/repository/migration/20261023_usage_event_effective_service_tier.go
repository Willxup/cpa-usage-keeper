package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addUsageEventEffectiveServiceTierMigration(tx *gorm.DB) error {
	// 历史事件缺少最终出站证据，保持空值，不从请求或响应档位推断回填。
	for _, table := range []struct {
		model any
		name  string
	}{
		{model: &entities.UsageEvent{}, name: "usage_events"},
		{model: &entities.UsageEventArchive{}, name: "usage_events_archive"},
	} {
		if !tx.Migrator().HasTable(table.model) || tx.Migrator().HasColumn(table.model, "effective_service_tier") {
			continue
		}
		if err := tx.Migrator().AddColumn(table.model, "EffectiveServiceTier"); err != nil {
			return fmt.Errorf("add %s.effective_service_tier column: %w", table.name, err)
		}
	}
	return nil
}
