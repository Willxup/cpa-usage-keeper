package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// addQuotaHistoryPersistedStatsMigration 为限额历史新增派生统计列，并扩充聚合水位表约束以支持 quota_history。
// 启动阶段只执行 O(1) DDL 与 3 行小表重建，绝不扫描 usage_events 或 usage_events_archive。
func addQuotaHistoryPersistedStatsMigration(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}

	if tx.Migrator().HasTable(&entities.QuotaCycle{}) {
		for _, field := range []string{
			"Requests",
			"SuccessfulRequests",
			"FailedRequests",
			"InputTokens",
			"OutputTokens",
			"ReasoningTokens",
			"CacheReadTokens",
			"CacheCreationTokens",
			"TotalTokens",
			"CostUSD",
			"UnavailableCostCount",
			"BoundaryVersion",
			"DirtyVersion",
			"UsageReady",
			"Dirty",
			"CostStale",
		} {
			if tx.Migrator().HasColumn(&entities.QuotaCycle{}, field) {
				continue
			}
			if err := tx.Migrator().AddColumn(&entities.QuotaCycle{}, field); err != nil {
				return fmt.Errorf("add quota_cycles.%s column: %w", field, err)
			}
		}
		if !tx.Migrator().HasIndex(&entities.QuotaCycle{}, "idx_quota_cycles_usage_sync") {
			if err := tx.Migrator().CreateIndex(&entities.QuotaCycle{}, "idx_quota_cycles_usage_sync"); err != nil {
				return fmt.Errorf("create idx_quota_cycles_usage_sync: %w", err)
			}
		}
	}

	if tx.Migrator().HasTable(&entities.QuotaPercentSegment{}) {
		for _, field := range []string{
			"TransitionRequests",
			"TransitionSuccessfulRequests",
			"TransitionFailedRequests",
			"TransitionInputTokens",
			"TransitionOutputTokens",
			"TransitionReasoningTokens",
			"TransitionCacheReadTokens",
			"TransitionCacheCreationTokens",
			"TransitionTotalTokens",
			"TransitionCostUSD",
			"TransitionUnavailableCostCount",
			"TransitionUsageReady",
		} {
			if tx.Migrator().HasColumn(&entities.QuotaPercentSegment{}, field) {
				continue
			}
			if err := tx.Migrator().AddColumn(&entities.QuotaPercentSegment{}, field); err != nil {
				return fmt.Errorf("add quota_percent_segments.%s column: %w", field, err)
			}
		}
	}

	if tx.Migrator().HasTable(&entities.UsageAggregationCheckpoint{}) {
		if err := rebuildUsageAggregationCheckpointsWithQuotaHistory(tx); err != nil {
			return err
		}
	}

	return nil
}

// rebuildUsageAggregationCheckpointsWithQuotaHistory 在单事务内重建 3 行小表，扩充 CHECK 约束并保留既有水位行。
func rebuildUsageAggregationCheckpointsWithQuotaHistory(tx *gorm.DB) error {
	var existing []entities.UsageAggregationCheckpoint
	if err := tx.Order("name ASC").Find(&existing).Error; err != nil {
		return fmt.Errorf("load existing usage aggregation checkpoints: %w", err)
	}

	if err := tx.Migrator().DropTable(&entities.UsageAggregationCheckpoint{}); err != nil {
		return fmt.Errorf("drop legacy usage aggregation checkpoints table: %w", err)
	}
	if err := tx.AutoMigrate(&entities.UsageAggregationCheckpoint{}); err != nil {
		return fmt.Errorf("recreate usage aggregation checkpoints table: %w", err)
	}

	for _, row := range existing {
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("restore usage aggregation checkpoint %q: %w", row.Name, err)
		}
	}

	now := timeutil.NormalizeStorageTime(time.Now())
	seed := entities.UsageAggregationCheckpoint{
		Name:                       entities.UsageAggregationCheckpointQuotaHistory,
		LastAggregatedUsageEventID: 0,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return fmt.Errorf("seed quota_history usage aggregation checkpoint: %w", err)
	}
	return nil
}
