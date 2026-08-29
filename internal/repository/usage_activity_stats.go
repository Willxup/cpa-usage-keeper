package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/activitystore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageActivityAggregationCheckpointName 保持兼容调用方使用的 Activity 名称不变。
	usageActivityAggregationCheckpointName = "activity"
	// usageActivityAggregationBatchSize 限制兼容入口每页最多读取 1000 个事件。
	usageActivityAggregationBatchSize = 1000
)

// AggregateUsageActivityStats 循环执行有界 batch，供完整 catch-up 和测试复用。
func AggregateUsageActivityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 整次 catch-up 固定 now，避免不同批次的 retention cutoff 漂移。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	for {
		processed, err := AggregateUsageActivityStatsBatch(ctx, db, normalizedNow)
		if err != nil {
			return err
		}
		if processed < usageActivityAggregationBatchSize {
			return nil
		}
	}
}

// AggregateUsageActivityStatsBatch 执行一次 Reader 事件页和一次独立 Activity 写事务。
func AggregateUsageActivityStatsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 兼容 batch 固定开始时可见的 target，再从自己的通用 cursor 读取一页。
	targetEventID, err := LoadUsageAggregationTargetEventID(ctx, db)
	if err != nil {
		return 0, err
	}
	snapshot, err := LoadUsageAggregationCheckpointSnapshot(ctx, db)
	if err != nil {
		return 0, err
	}
	events, err := LoadUsageAggregationEventPage(ctx, db, snapshot.ActivityCursor, targetEventID, usageActivityAggregationBatchSize)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}

	// Activity 纯计算仍由原 BuildRows 唯一负责，避免 migration/runtime 语义分叉。
	rows, err := activity.BuildRows(events, normalizedNow)
	if err != nil {
		return 0, err
	}
	nextCursor := events[len(events)-1].ID
	if err := ApplyUsageActivityAggregationPage(ctx, db, snapshot.ActivityCursor, nextCursor, rows, normalizedNow); err != nil {
		return 0, err
	}
	return len(events), nil
}

// ApplyUsageActivityAggregationPage 在独立短事务内写 rows 并条件推进 Activity 水位。
func ApplyUsageActivityAggregationPage(ctx context.Context, db *gorm.DB, expectedCursor, nextCursor int64, rows []entities.UsageActivityStat, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 复用既有 Activity upsert；事务边界只新增通用 cursor 条件推进。
		if err := activitystore.ApplyRows(tx, rows, normalizedNow); err != nil {
			return err
		}
		// cursor 冲突必须回滚本页所有 grain rows，防止下一轮重复累计。
		return AdvanceUsageAggregationCheckpoint(ctx, tx, entities.UsageAggregationCheckpointActivity, expectedCursor, nextCursor, normalizedNow)
	})
}

// HasPendingUsageActivityAggregation 比较 Reader target 与已提交 Activity cursor。
func HasPendingUsageActivityAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	targetEventID, err := LoadUsageAggregationTargetEventID(ctx, db)
	if err != nil {
		return false, err
	}
	if targetEventID == 0 {
		return false, nil
	}
	snapshot, err := LoadUsageAggregationCheckpointSnapshot(ctx, db)
	if err != nil {
		return false, err
	}
	return snapshot.ActivityCursor < targetEventID, nil
}

// CleanupUsageActivityStats 按 grain 的 bucket_end 清理短期 rows，并永久保留 daily。
func CleanupUsageActivityStats(db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 固定本轮 cleanup now，避免三个 DELETE 使用不同 cutoff。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	for _, grain := range []entities.UsageActivityGrain{
		entities.UsageActivityGrainShort,
		entities.UsageActivityGrainMedium,
		entities.UsageActivityGrainLong,
	} {
		// retention 与 BuildRows 共享 activity 包定义，daily 不进入该列表。
		retention, limited := activity.Retention(grain)
		if !limited {
			continue
		}
		cutoff := normalizedNow.Add(-retention)
		if err := db.Where("grain = ? AND bucket_end < ?", grain, timeutil.FormatSortableStorageTime(cutoff)).Delete(&entities.UsageActivityStat{}).Error; err != nil {
			return fmt.Errorf("cleanup usage activity %s stats: %w", grain, err)
		}
	}
	return nil
}
