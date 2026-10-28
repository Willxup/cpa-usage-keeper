package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func addPluginAPIKeySourcesMigration(tx *gorm.DB) error {
	// 只定向增列；旧用量保持未知来源，旧 key 由列默认值初始化为 native。
	for _, table := range []struct {
		model  any
		fields []string
	}{
		{&entities.UsageEvent{}, []string{"IsNativeKey", "AccessProvider"}},
		{&entities.UsageEventArchive{}, []string{"IsNativeKey", "AccessProvider"}},
		{&entities.CPAAPIKey{}, []string{"SourceType", "AccessProvider"}},
	} {
		if !tx.Migrator().HasTable(table.model) {
			continue
		}
		for _, field := range table.fields {
			if tx.Migrator().HasColumn(table.model, field) {
				continue
			}
			if err := tx.Migrator().AddColumn(table.model, field); err != nil {
				return fmt.Errorf("add API key source field %s: %w", field, err)
			}
		}
	}
	return nil
}
