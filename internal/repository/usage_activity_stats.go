package repository

import (
	"context"
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
	// usageActivityAggregationCheckpointName 固定运行时 Activity 的独立 cursor 名称。
	usageActivityAggregationCheckpointName = "activity"
	// usageActivityAggregationBatchSize 限制每个生产事务最多读取 1000 个 usage events。
	usageActivityAggregationBatchSize = 1000
)

// AggregateUsageActivityStats 循环执行有界 batch，供 migration 外的完整 catch-up 和测试使用。
func AggregateUsageActivityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	// nil 数据库属于调用错误，必须在进入事务前返回。
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 整次 catch-up 固定同一个 now，避免不同 batch 的 retention cutoff 漂移。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 每轮只提交一个短事务，直到最后一批少于固定上限。
	for {
		// 生产 runner 后续只调用单批入口；完整入口负责显式循环。
		processed, err := AggregateUsageActivityStatsBatch(ctx, db, normalizedNow)
		// 任一 batch 失败立即停止，已提交 checkpoint 保留供下次恢复。
		if err != nil {
			return err
		}
		// 少于满批说明当前 checkpoint 已追平事务开始时可见的新事件。
		if processed < usageActivityAggregationBatchSize {
			return nil
		}
	}
}

// AggregateUsageActivityStatsBatch 在一个短事务内写 Activity rows 并推进独立 checkpoint。
func AggregateUsageActivityStatsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	// nil 数据库不能开启 GORM 事务。
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	// 单批也归一化 now，保证生产 runner 直接调用时使用统一时区。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// processed 只在事务内所有写入成功后赋值。
	processed := 0
	// 事件读取、Activity upsert 和 checkpoint 推进必须原子提交。
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 只读取或创建 name=activity 的 checkpoint，不触碰 Overview cursor。
		checkpoint, err := getOrCreateUsageActivityAggregationCheckpoint(tx)
		if err != nil {
			return err
		}

		// SELECT 批次使用独立常量，不借实体列数反推。
		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ?", checkpoint.LastAggregatedUsageEventID).
			Order("id asc").
			Limit(usageActivityAggregationBatchSize).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage activity aggregation events: %w", err)
		}
		// 没有新事件时保持 checkpoint 不变并结束空事务。
		if len(events) == 0 {
			return nil
		}

		// migration 与运行时共同调用唯一事件到 Activity rows 的聚合实现。
		rows, err := activity.BuildRows(events, normalizedNow)
		if err != nil {
			return err
		}
		// migration 与运行时共同调用唯一 GORM upsert 实现。
		if err := activitystore.ApplyRows(tx, rows, normalizedNow); err != nil {
			return err
		}

		// events 按 ID 升序读取，最后一行是本 batch 的安全 cursor。
		maxEventID := events[len(events)-1].ID
		// checkpoint 与 Activity rows 同事务提交，失败时不会形成半聚合状态。
		if err := tx.Model(&entities.UsageActivityAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				// cursor 只推进到当前事务已经写完的最大事件 ID。
				"last_aggregated_usage_event_id": maxEventID,
				// stats_updated_at 使用本批固定的规范化时间。
				"stats_updated_at": timeutil.FormatStorageTime(normalizedNow),
			}).Error; err != nil {
			return fmt.Errorf("update usage activity aggregation checkpoint: %w", err)
		}
		// 事务函数成功返回前记录真实处理数。
		processed = len(events)
		return nil
	})
	// 事务提交失败时上层会忽略 processed 并根据 error 重试。
	return processed, err
}

// HasPendingUsageActivityAggregation 用轻量 MAX(id) 与独立 cursor 判断是否需要调度 batch。
func HasPendingUsageActivityAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	// nil 数据库无法执行 pending 查询。
	if db == nil {
		return false, fmt.Errorf("database is nil")
	}
	// 只读取 usage_events 最大 ID，不扫描事件内容。
	var maxEventID int64
	if err := db.WithContext(ctx).Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxEventID).Error; err != nil {
		return false, fmt.Errorf("load max usage event id for activity: %w", err)
	}
	// 空事件表无需创建 checkpoint 或唤醒 runner。
	if maxEventID == 0 {
		return false, nil
	}

	// Activity checkpoint 不存在时，任何已有 event 都表示需要从头追平。
	var checkpoint entities.UsageActivityAggregationCheckpoint
	err := db.WithContext(ctx).Where("name = ?", usageActivityAggregationCheckpointName).Take(&checkpoint).Error
	// 未找到独立 cursor 是正常 pending 状态，不当作数据库错误。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	// 其它读取错误必须返回给调度器记录和退避。
	if err != nil {
		return false, fmt.Errorf("load usage activity aggregation checkpoint: %w", err)
	}
	// cursor 落后最大 event ID 时仍有至少一个 batch 待处理。
	return checkpoint.LastAggregatedUsageEventID < maxEventID, nil
}

// CleanupUsageActivityStats 按 grain 的 bucket_end 清理短期 rows，并永久保留 daily。
func CleanupUsageActivityStats(db *gorm.DB, now time.Time) error {
	// nil 数据库无法执行 cleanup。
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 固定本轮 cleanup now，避免三个 DELETE 使用不同 cutoff。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 只枚举有 retention 的三个短 grain，daily 不进入删除集合。
	for _, grain := range []entities.UsageActivityGrain{
		entities.UsageActivityGrainShort,
		entities.UsageActivityGrainMedium,
		entities.UsageActivityGrainLong,
	} {
		// retention 与 migration/运行时 BuildRows 共享 activity 包定义。
		retention, limited := activity.Retention(grain)
		// 固定列表中的 grain 理论上都有限制；防御性跳过未知值。
		if !limited {
			continue
		}
		// bucket_end 早于 cutoff 的完整 bucket 已无查询价值。
		cutoff := normalizedNow.Add(-retention)
		// DELETE 同时带 grain 和 bucket_end，确保不会误删其它层级或 daily。
		if err := db.Where("grain = ? AND bucket_end < ?", grain, timeutil.FormatSortableStorageTime(cutoff)).Delete(&entities.UsageActivityStat{}).Error; err != nil {
			return fmt.Errorf("cleanup usage activity %s stats: %w", grain, err)
		}
	}
	// 三个短 grain 清理完成，daily 始终保留。
	return nil
}

func getOrCreateUsageActivityAggregationCheckpoint(tx *gorm.DB) (entities.UsageActivityAggregationCheckpoint, error) {
	// 唯一 name 确保运行时与 migration 在升级后继续同一个 Activity cursor。
	checkpoint := entities.UsageActivityAggregationCheckpoint{Name: usageActivityAggregationCheckpointName}
	// FirstOrCreate 在空库首次聚合时从 0 初始化，之后读取已提交 cursor。
	if err := tx.Where("name = ?", usageActivityAggregationCheckpointName).FirstOrCreate(&checkpoint).Error; err != nil {
		return checkpoint, fmt.Errorf("get usage activity aggregation checkpoint: %w", err)
	}
	// 返回当前 cursor 供本 batch 构造 id > checkpoint 查询。
	return checkpoint, nil
}
