package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// createUsageOverviewStatsMigration 重放 2026-05-14 的历史 Overview schema；其中旧 Health 表会由后续 Activity migration 迁移并删除。
func createUsageOverviewStatsMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&entities.UsageOverviewHourlyStat{},
		&entities.UsageOverviewDailyStat{},
		// 历史 migration 使用包内 legacy 结构，避免最终业务 entity 继续暴露旧 Health 表。
		&legacyUsageOverviewHealthStat{},
		&entities.UsageOverviewAggregationCheckpoint{},
	); err != nil {
		return fmt.Errorf("auto migrate usage overview stats: %w", err)
	}
	// 查询侧按时间窗口、API key 和模型组合读取 stats，因此这里显式补齐复合索引。
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias ON usage_overview_hourly_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_hourly_stats_bucket_start ON usage_overview_hourly_stats (bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_hourly_stats_api_bucket ON usage_overview_hourly_stats (api_group_key, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_hourly_stats_api_model_bucket ON usage_overview_hourly_stats (api_group_key, model, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_hourly_stats_auth_bucket ON usage_overview_hourly_stats (auth_index, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_hourly_stats_model_alias_bucket ON usage_overview_hourly_stats (model_alias, bucket_start)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_daily_stats_bucket_api_model_auth_alias ON usage_overview_daily_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_daily_stats_bucket_start ON usage_overview_daily_stats (bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_daily_stats_api_bucket ON usage_overview_daily_stats (api_group_key, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_daily_stats_api_model_bucket ON usage_overview_daily_stats (api_group_key, model, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_daily_stats_auth_bucket ON usage_overview_daily_stats (auth_index, bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_daily_stats_model_alias_bucket ON usage_overview_daily_stats (model_alias, bucket_start)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_health_stats_bucket_span_api ON usage_overview_health_stats (bucket_start, span_seconds, api_group_key)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_health_stats_bucket_start ON usage_overview_health_stats (bucket_start)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_overview_health_stats_api_bucket_span ON usage_overview_health_stats (api_group_key, bucket_start, span_seconds)`,
	} {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create usage overview stats index: %w", err)
		}
	}
	return nil
}
