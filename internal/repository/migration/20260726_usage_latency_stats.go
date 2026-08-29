package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageLatencyMigrationBatchSize 与运行时事件页上限一致，避免回填长时间占用唯一 writer。
	usageLatencyMigrationBatchSize = 1000
)

// usageLatencyStatsMigration 从当前仍保留的 usage_events 回填 hour/day Latency 聚合。
func usageLatencyStatsMigration(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 建表独立提交；后续任一数据页失败时，已经完成的页面和 cursor 都必须保留。
	if err := db.AutoMigrate(&entities.UsageLatencyStat{}); err != nil {
		return fmt.Errorf("create usage latency stats schema: %w", err)
	}

	// migration 只追启动时已经可见的最大 ID，避免持续写入时永远无法完成升级。
	var targetEventID int64
	if db.Migrator().HasTable(&entities.UsageEvent{}) {
		if err := db.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&targetEventID).Error; err != nil {
			return fmt.Errorf("load usage latency migration target: %w", err)
		}
	}
	// 复用数据库时钟，让 migration 与 GORM 写入及可重复测试共享同一时间来源。
	now := timeutil.NormalizeStorageTime(db.NowFunc())

	// 每页是一个独立短事务；重启时直接从已提交的 Latency checkpoint 继续。
	for {
		processed, err := migrateUsageLatencyBatch(db, now, targetEventID)
		if err != nil {
			return err
		}
		if processed == 0 {
			break
		}
	}
	return verifyUsageLatencyMigrationTarget(db, targetEventID)
}

func migrateUsageLatencyBatch(db *gorm.DB, now time.Time, targetEventID int64) (int, error) {
	processed := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		// 通用 checkpoint migration 已预置 Latency 行；缺行说明升级前置条件不完整，不能猜测 cursor。
		var checkpoint entities.UsageAggregationCheckpoint
		if err := tx.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
			return fmt.Errorf("load usage latency migration checkpoint: %w", err)
		}
		if checkpoint.LastAggregatedUsageEventID >= targetEventID {
			return nil
		}

		// migration 与运行时共享同一固定字段投影，避免历史回填悄悄形成第二套数据契约。
		var events []entities.UsageEvent
		if err := tx.Select(entities.UsageAggregationEventProjectionColumns).
			Where("id > ? AND id <= ?", checkpoint.LastAggregatedUsageEventID, targetEventID).
			Order("id asc").
			Limit(usageLatencyMigrationBatchSize).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage latency migration events: %w", err)
		}
		if len(events) == 0 {
			return nil
		}

		// Sketch、真实点和 hour/day 分桶只由 latency 包实现，migration 不复制算法。
		rows, err := latency.BuildRows(events, now)
		if err != nil {
			return fmt.Errorf("build usage latency migration rows: %w", err)
		}
		// Store 合并与运行时共用，保证 BLOB 格式、计数和最大值不会因入口不同而分叉。
		if err := latencystore.ApplyRows(tx, rows, now); err != nil {
			return err
		}

		nextCursor := events[len(events)-1].ID
		// name + expected cursor 是恢复安全边界；水位被并发推进时整页回滚，禁止重复累计。
		result := tx.Model(&entities.UsageAggregationCheckpoint{}).
			Where("name = ? AND last_aggregated_usage_event_id = ?", entities.UsageAggregationCheckpointLatency, checkpoint.LastAggregatedUsageEventID).
			Updates(map[string]any{
				"last_aggregated_usage_event_id": nextCursor,
				"stats_updated_at":               timeutil.FormatStorageTime(now),
				"updated_at":                     timeutil.FormatStorageTime(now),
			})
		if result.Error != nil {
			return fmt.Errorf("advance usage latency migration checkpoint: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("advance usage latency migration checkpoint: expected cursor %d changed", checkpoint.LastAggregatedUsageEventID)
		}
		processed = len(events)
		return nil
	})
	return processed, err
}

func verifyUsageLatencyMigrationTarget(db *gorm.DB, targetEventID int64) error {
	// 即使 target 为 0，也验证通用 migration 预置的 Latency 行仍然存在。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
		return fmt.Errorf("verify usage latency migration checkpoint: %w", err)
	}
	if checkpoint.LastAggregatedUsageEventID < targetEventID {
		return fmt.Errorf("usage latency migration checkpoint %d did not reach target %d", checkpoint.LastAggregatedUsageEventID, targetEventID)
	}
	return nil
}
