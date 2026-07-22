package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository/overviewstore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageOverviewAggregationCheckpointName 保持现有 Overview cursor 名称不变。
	usageOverviewAggregationCheckpointName = "overview"
	// usageOverviewAggregationBatchSize 独立限制 Overview SELECT 批次，不再由实体列数反推。
	usageOverviewAggregationBatchSize = 1000
)

// AggregateUsageOverviewStats 按 usage_events 自增 ID 增量推进既有 Overview 小时/天统计。
func AggregateUsageOverviewStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	// nil 数据库无法执行 Overview catch-up。
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 整次 catch-up 固定同一个项目时区 now，保持旧时间语义不变。
	now = timeutil.NormalizeStorageTime(now)
	// 每轮只执行一个最多 1000 events 的事务，直到 checkpoint 追平。
	for {
		// Overview 继续使用原事务函数，只把 SELECT limit 固定为独立常量。
		processed, err := AggregateUsageOverviewStatsBatch(ctx, db, now)
		// 任一 batch 失败立即停止，已提交旧表结果保持可恢复。
		if err != nil {
			return err
		}
		// 少于满批表示当前没有更多未聚合事件。
		if processed < usageOverviewAggregationBatchSize {
			return nil
		}
	}
}

// AggregateUsageOverviewStatsBatch 只执行一个有界 Overview 写事务，供低优先级 runner 公平调度。
func AggregateUsageOverviewStatsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	// nil 数据库不能进入 Overview 事务。
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	// 单批入口统一使用项目存储时区，避免 runner 传入值造成 bucket 语义漂移。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 复用原有事务实现，并固定使用 Overview 自己的 1000-event 上限。
	return aggregateUsageOverviewStatsBatch(ctx, db, normalizedNow, usageOverviewAggregationBatchSize)
}

// UsageOverviewAggregationCursor 返回已提交 Overview checkpoint，供 header snapshot gate 判断。
func UsageOverviewAggregationCursor(ctx context.Context, db *gorm.DB) (int64, error) {
	// nil 数据库无法读取 Overview cursor。
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	// 只读取唯一 name=overview 行，避免把其它 checkpoint 误作 header gate。
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	// checkpoint 尚未创建表示 Overview 仍停留在 event ID 0。
	err := db.WithContext(ctx).Where("name = ?", usageOverviewAggregationCheckpointName).Take(&checkpoint).Error
	// 首轮聚合前没有 checkpoint 是正常状态，不返回数据库错误。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	// 其它查询错误交给 runner 记录并重试，不能提前投递 header snapshot。
	if err != nil {
		return 0, fmt.Errorf("load usage overview aggregation cursor: %w", err)
	}
	// 只返回事务已经提交的最大 usage event ID。
	return checkpoint.LastAggregatedUsageEventID, nil
}

// HasPendingUsageOverviewAggregation 用轻量 ID cursor 判断 Overview stats 是否落后，避免空轮次每秒跑完整聚合。
func HasPendingUsageOverviewAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	// nil 数据库无法读取 event/cursor 状态。
	if db == nil {
		return false, fmt.Errorf("database is nil")
	}
	// 只读取 usage_events 最大 ID，不加载事件内容。
	var maxEventID int64
	// MAX 查询失败时不能猜测 Overview 已追平。
	if err := db.WithContext(ctx).Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxEventID).Error; err != nil {
		return false, fmt.Errorf("load max usage event id: %w", err)
	}
	// 空事件表没有待聚合工作。
	if maxEventID == 0 {
		return false, nil
	}

	// 读取固定 name=overview 的旧 checkpoint。
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	// 查询只观察已提交 cursor，不创建新行。
	err := db.WithContext(ctx).Where("name = ?", usageOverviewAggregationCheckpointName).Take(&checkpoint).Error
	// checkpoint 不存在且已有事件表示必须从头聚合。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	// 其它查询错误交给调用方处理。
	if err != nil {
		return false, fmt.Errorf("load usage overview aggregation checkpoint: %w", err)
	}
	// 旧 cursor 小于最大 event ID 时仍有待处理数据。
	return checkpoint.LastAggregatedUsageEventID < maxEventID, nil
}

// aggregateUsageOverviewStatsBatch 在一个事务里读取新事件、累计 stats，并推进 checkpoint。
func aggregateUsageOverviewStatsBatch(ctx context.Context, db *gorm.DB, now time.Time, limit int) (int, error) {
	// processed 只在 hourly、daily 和 Overview checkpoint 全部成功后赋值。
	processed := 0
	// 保留原有单事务语义，确保两个旧表和旧 checkpoint 最终效果不变。
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Overview 继续读取自己的 name=overview checkpoint。
		checkpoint, err := getOrCreateUsageOverviewAggregationCheckpoint(tx)
		if err != nil {
			return err
		}

		// SELECT 明确保留全部旧 hourly/daily 累计字段，包括 cached_tokens。
		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, model, model_alias, auth_index, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ?", checkpoint.LastAggregatedUsageEventID).
			Order("id asc").
			Limit(limit).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage overview aggregation events: %w", err)
		}
		// 没有新事件时不推进旧 checkpoint。
		if len(events) == 0 {
			processed = 0
			return nil
		}

		// migration 与运行时共用同一个五维分组实现，避免重建后增量语义漂移。
		hourlyRows, dailyRows, maxEventID := overview.BuildRows(events)
		// hourly、daily 必须通过同一写入层在当前事务内一起完成。
		if err := overviewstore.ApplyRows(tx, hourlyRows, dailyRows, now); err != nil {
			return err
		}
		// 只有两个旧表都成功后才推进原 Overview checkpoint。
		if err := tx.Model(&entities.UsageOverviewAggregationCheckpoint{}).
			Where("id = ?", checkpoint.ID).
			Updates(map[string]any{
				// 旧 cursor 只推进到本事务两个旧表都已写完的最大 ID。
				"last_aggregated_usage_event_id": maxEventID,
				// 旧 stats_updated_at 继续使用本批固定 now。
				"stats_updated_at": timeutil.FormatStorageTime(now),
			}).Error; err != nil {
			return fmt.Errorf("update usage overview aggregation checkpoint: %w", err)
		}
		// 事务函数成功返回前记录本批处理数。
		processed = len(events)
		return nil
	})
	// 返回原 Overview 事务结果，Activity 成败不参与这里的提交。
	return processed, err
}

// getOrCreateUsageOverviewAggregationCheckpoint 读取 Overview 的唯一 cursor 行，不存在时初始化为从头聚合。
func getOrCreateUsageOverviewAggregationCheckpoint(tx *gorm.DB) (entities.UsageOverviewAggregationCheckpoint, error) {
	// 固定旧 checkpoint 名称，不能因 Activity 新增而改名。
	checkpoint := entities.UsageOverviewAggregationCheckpoint{Name: usageOverviewAggregationCheckpointName}
	// 首次聚合从 0 创建，后续批次读取已提交旧 cursor。
	if err := tx.Where("name = ?", usageOverviewAggregationCheckpointName).FirstOrCreate(&checkpoint).Error; err != nil {
		return checkpoint, fmt.Errorf("get usage overview aggregation checkpoint: %w", err)
	}
	// 返回当前旧 cursor 供本 batch 使用。
	return checkpoint, nil
}
