package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// addPricingStorageStructureMigration 只补费用、速度与控制结构，不回填数据或宣告就绪。
// 费用列保持 NULL，速度列从零开始；首次升级随后按固定基线同轮重建完整 Overview。
func addPricingStorageStructureMigration(tx *gorm.DB) error {
	for _, target := range []struct {
		table   string
		model   any
		columns []struct{ name, field string }
	}{
		{"usage_events", &entities.UsageEvent{}, []struct{ name, field string }{{"cost_usd", "CostUSD"}, {"cost_available", "CostAvailable"}}},
		{"usage_events_archive", &entities.UsageEventArchive{}, []struct{ name, field string }{{"cost_usd", "CostUSD"}, {"cost_available", "CostAvailable"}}},
		{"usage_overview_hourly_stats", &entities.UsageOverviewHourlyStat{}, overviewPricingColumns()},
		{"usage_overview_daily_stats", &entities.UsageOverviewDailyStat{}, overviewPricingColumns()},
		{"model_price_settings", &entities.ModelPriceSetting{}, []struct{ name, field string }{{"branches_json", "BranchesJSON"}}},
	} {
		if !tx.Migrator().HasTable(target.table) {
			// 缺表代表没有旧行；只在此时建立当前完整结构，已有表始终逐列增补。
			if err := tx.Migrator().CreateTable(target.model); err != nil {
				return fmt.Errorf("create pricing storage table %s: %w", target.table, err)
			}
			continue
		}
		for _, column := range target.columns {
			if tx.Migrator().HasColumn(target.table, column.name) {
				continue
			}
			if err := tx.Migrator().AddColumn(target.model, column.field); err != nil {
				return fmt.Errorf("add %s.%s: %w", target.table, column.name, err)
			}
		}
	}
	for _, target := range []struct {
		table string
		model any
	}{
		{"pricing_state", &entities.PricingState{}},
		{"pricing_migration_state", &entities.PricingMigrationState{}},
	} {
		if tx.Migrator().HasTable(target.table) {
			continue
		}
		if err := tx.Migrator().CreateTable(target.model); err != nil {
			return fmt.Errorf("create pricing control table %s: %w", target.table, err)
		}
	}
	return nil
}

// 未发布 v2 的结构阶段一次准备费用与速度列，后续 M5 从现存明细同轮重建。
func overviewPricingColumns() []struct{ name, field string } {
	return []struct{ name, field string }{
		{"cost_usd", "CostUSD"}, {"unavailable_cost_count", "UnavailableCostCount"},
		{"speed_tps_sum", "SpeedTPSSum"}, {"speed_sample_count", "SpeedSampleCount"},
		{"decode_speed_tps_sum", "DecodeSpeedTPSSum"}, {"decode_speed_sample_count", "DecodeSpeedSampleCount"},
	}
}
