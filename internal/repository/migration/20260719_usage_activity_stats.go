package migration

import (
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/activitystore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageActivityMigrationBatchSize 把历史回填限制为短事务，避免长时间占用 SQLite writer。
	usageActivityMigrationBatchSize = 1000
	// usageActivityCheckpointName 与运行时增量聚合共享同一个独立 cursor 名称。
	usageActivityCheckpointName = "activity"
)

// usageActivityStatsMigration 以可恢复小事务回填 Activity，并在完成后删除旧 Health 表。
func usageActivityStatsMigration(db *gorm.DB) error {
	// migration 开始时固定 now，确保所有 batch 使用相同 retention cutoff。
	now := timeutil.NormalizeStorageTime(time.Now())
	// 先幂等创建最终 Activity 表、checkpoint 和索引。
	if err := db.AutoMigrate(&entities.UsageActivityStat{}, &entities.UsageActivityAggregationCheckpoint{}); err != nil {
		return fmt.Errorf("create usage activity schema: %w", err)
	}

	// 没有 usage_events 的极旧数据库无需回填，只需要移除可能存在的旧 Health 表。
	if !db.Migrator().HasTable(&entities.UsageEvent{}) {
		return dropLegacyUsageOverviewHealthStats(db)
	}

	// migration 只处理开始时已经可见的最大 event ID，避免追逐并发新增数据。
	var targetEventID int64
	if err := db.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&targetEventID).Error; err != nil {
		return fmt.Errorf("load usage activity migration target: %w", err)
	}

	// 每轮只提交一个至多 1000 events 的事务，失败后保留上一个已提交 checkpoint。
	for {
		// 当前 batch 同时返回实际处理事件数，0 表示已经追平固定 target。
		processed, err := migrateUsageActivityBatch(db, now, targetEventID)
		// 任一 batch 失败立即停止，旧 Health 表和 migration version 都暂时保留。
		if err != nil {
			return err
		}
		// 没有更多 target 内事件时结束回填循环。
		if processed == 0 {
			break
		}
	}

	// 回填循环结束后必须重新读取已提交 checkpoint，不能把目标行消失误判为追平。
	if err := verifyUsageActivityMigrationTarget(db, targetEventID); err != nil {
		return err
	}
	// 只有 Activity checkpoint 已追平固定 target 后，才删除误导后续迭代的旧 Health 表。
	return dropLegacyUsageOverviewHealthStats(db)
}

func verifyUsageActivityMigrationTarget(db *gorm.DB, targetEventID int64) error {
	// migration 开始时没有 raw event 时不要求创建无意义 checkpoint。
	if targetEventID == 0 {
		return nil
	}
	// 非空 target 必须由 name=activity 的已提交 cursor 覆盖。
	var checkpoint entities.UsageActivityAggregationCheckpoint
	if err := db.Where("name = ?", usageActivityCheckpointName).Take(&checkpoint).Error; err != nil {
		return fmt.Errorf("verify usage activity migration checkpoint: %w", err)
	}
	// cursor 小于固定 target 表示回填并未完整处理 migration 开始时存在的数据。
	if checkpoint.LastAggregatedUsageEventID < targetEventID {
		return fmt.Errorf("usage activity migration checkpoint %d did not reach target %d", checkpoint.LastAggregatedUsageEventID, targetEventID)
	}
	// 只有达到或越过固定 target 才允许删除旧 Health 表并记录 migration version。
	return nil
}

func migrateUsageActivityBatch(db *gorm.DB, now time.Time, targetEventID int64) (int, error) {
	// processed 在事务提交后告诉外层是否需要继续下一批。
	processed := 0
	// 每批读取、Activity upsert 和 checkpoint 推进必须原子提交。
	err := db.Transaction(func(tx *gorm.DB) error {
		// Activity 使用独立 name=activity cursor，不读取或修改 Overview checkpoint。
		checkpoint, err := getOrCreateUsageActivityMigrationCheckpoint(tx)
		if err != nil {
			return err
		}
		// 已达到 migration 固定 target 时，本事务不再读取事件。
		if checkpoint.LastAggregatedUsageEventID >= targetEventID {
			return nil
		}

		// 只选择 Activity 所需字段，明确不读取 cached_tokens。
		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ? AND id <= ?", checkpoint.LastAggregatedUsageEventID, targetEventID).
			Order("id asc").
			Limit(usageActivityMigrationBatchSize).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage activity migration events: %w", err)
		}
		// 查不到新事件时先结束批次，外层随后验证 checkpoint 是否真正达到固定 target。
		if len(events) == 0 {
			return nil
		}

		// 先在内存按 grain/bucket/API group 合并，减少 SQLite upsert 次数。
		rows, err := activity.BuildRows(events, now)
		if err != nil {
			return err
		}
		// migration 与运行时共同调用唯一 Activity upsert，字段累计不能分叉。
		if err := activitystore.ApplyRows(tx, rows, now); err != nil {
			return err
		}

		// events 按 ID 升序读取，最后一行就是本 batch 可安全提交的最大 cursor。
		maxEventID := events[len(events)-1].ID
		// checkpoint 与 Activity rows 使用同一事务，失败时两者一起回滚。
		if err := tx.Model(&entities.UsageActivityAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				// migration cursor 只推进到当前已提交 batch 的最大 ID。
				"last_aggregated_usage_event_id": maxEventID,
				// migration 所有 batch 共用开始时固定 now。
				"stats_updated_at": timeutil.FormatStorageTime(now),
			}).Error; err != nil {
			return fmt.Errorf("update usage activity migration checkpoint: %w", err)
		}
		// 只有事务内所有写入成功后才记录本批处理数量。
		processed = len(events)
		return nil
	})
	// 返回事务结果；失败时 processed 的值不代表已提交进度。
	return processed, err
}

func getOrCreateUsageActivityMigrationCheckpoint(tx *gorm.DB) (entities.UsageActivityAggregationCheckpoint, error) {
	// 通过唯一 name 初始化或读取 Activity 自己的 cursor 行。
	checkpoint := entities.UsageActivityAggregationCheckpoint{Name: usageActivityCheckpointName}
	// FirstOrCreate 保持 migration 中断重跑时复用同一 checkpoint。
	if err := tx.Where("name = ?", usageActivityCheckpointName).FirstOrCreate(&checkpoint).Error; err != nil {
		return checkpoint, fmt.Errorf("get usage activity migration checkpoint: %w", err)
	}
	// 返回当前已提交 cursor，供 batch 查询下一个 ID 范围。
	return checkpoint, nil
}

func dropLegacyUsageOverviewHealthStats(db *gorm.DB) error {
	// 已删除或从未存在时直接成功，保证 migration version 写入前重跑幂等。
	if !db.Migrator().HasTable("usage_overview_health_stats") {
		return nil
	}
	// 使用 GORM migrator 删除旧表，避免在业务代码继续保留 Health entity。
	if err := db.Migrator().DropTable("usage_overview_health_stats"); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("drop legacy usage overview health stats: %w", err)
	}
	// 旧 Health 表删除成功后，外层才能记录 migration version。
	return nil
}
