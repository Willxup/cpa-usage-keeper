package migration

import (
	"fmt"

	"gorm.io/gorm"
)

func usageOverviewServiceTierMigration(db *gorm.DB) error {
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if !tx.Migrator().HasColumn(table, "service_tier") {
				if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN service_tier TEXT NOT NULL DEFAULT 'default'", table)).Error; err != nil {
					return fmt.Errorf("add %s.service_tier column: %w", table, err)
				}
			}
		}
		if tx.Migrator().HasTable("usage_overview_hourly_stats") && tx.Migrator().HasTable("usage_overview_daily_stats") {
			if err := replaceUsageOverviewServiceTierIndexes(tx); err != nil {
				return err
			}
		}
		for _, table := range []string{
			"usage_overview_hourly_stats",
			"usage_overview_daily_stats",
			"usage_overview_health_stats",
			"usage_overview_aggregation_checkpoints",
		} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := db.Exec("VACUUM").Error; err != nil {
		return fmt.Errorf("vacuum usage overview service tier migration: %w", err)
	}
	return nil
}

func replaceUsageOverviewServiceTierIndexes(db *gorm.DB) error {
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias`,
		`DROP INDEX IF EXISTS uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias`,
		`DROP INDEX IF EXISTS uniq_usage_overview_daily_stats_bucket_api_model_auth_alias`,
		`DROP INDEX IF EXISTS uniq_usage_overview_daily_stats_bucket_api_model_tier_auth_alias`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_hourly_stats_bucket_api_model_tier_auth_alias ON usage_overview_hourly_stats (bucket_start, api_group_key, model, service_tier, auth_index, model_alias)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_daily_stats_bucket_api_model_tier_auth_alias ON usage_overview_daily_stats (bucket_start, api_group_key, model, service_tier, auth_index, model_alias)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("replace usage overview service tier index: %w", err)
		}
	}
	return nil
}
