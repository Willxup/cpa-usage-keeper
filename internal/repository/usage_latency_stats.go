package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const usageLatencyAggregationBatchSize = 1000

// AggregateUsageLatencyStats 循环执行有界页面，供没有后台 Runner 的兼容路径完整追赶。
func AggregateUsageLatencyStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	for {
		processed, err := AggregateUsageLatencyStatsBatch(ctx, db, normalizedNow)
		if err != nil {
			return err
		}
		if processed < usageLatencyAggregationBatchSize {
			return nil
		}
	}
}

// AggregateUsageLatencyStatsBatch 读取一页事件、在内存构建 Latency rows，再用独立事务推进水位。
func AggregateUsageLatencyStatsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	targetEventID, err := LoadUsageAggregationTargetEventID(ctx, db)
	if err != nil {
		return 0, err
	}
	snapshot, err := LoadUsageAggregationCheckpointSnapshot(ctx, db)
	if err != nil {
		return 0, err
	}
	events, err := LoadUsageAggregationEventPage(ctx, db, snapshot.LatencyCursor, targetEventID, usageLatencyAggregationBatchSize)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	rows, err := latency.BuildRows(events, now)
	if err != nil {
		return 0, err
	}
	nextCursor := events[len(events)-1].ID
	if err := ApplyUsageLatencyAggregationPage(ctx, db, snapshot.LatencyCursor, nextCursor, rows, now); err != nil {
		return 0, err
	}
	return len(events), nil
}

// CleanupUsageLatencyStats 删除页面查询范围之外的 hour/day 行，防止聚合 BLOB 跨年增长。
func CleanupUsageLatencyStats(db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	for _, bucketType := range []entities.UsageLatencyBucketType{entities.UsageLatencyBucketHour, entities.UsageLatencyBucketDay} {
		cutoff, err := latency.RetentionBucketStartCutoff(bucketType, now)
		if err != nil {
			return err
		}
		// bucket_start 使用 storageTime 保存项目本地墙钟时间；清理参数必须保持同一种文本格式。
		if err := db.Where("bucket_type = ? AND bucket_start < ?", bucketType, timeutil.FormatStorageTime(cutoff)).Delete(&entities.UsageLatencyStat{}).Error; err != nil {
			return fmt.Errorf("cleanup usage latency %s stats: %w", bucketType, err)
		}
	}
	return nil
}

// ApplyUsageLatencyAggregationPage 在独立短事务内合并 Latency rows 并推进自己的水位。
func ApplyUsageLatencyAggregationPage(ctx context.Context, db *gorm.DB, expectedCursor, nextCursor int64, rows []entities.UsageLatencyStat, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 旧行查询、解码、Sketch 合并和样本裁剪全部走 Reader，并在启动 writer 事务前结束。
	preparedRows, err := latencystore.PrepareRows(db.Clauses(dbresolver.Read).WithContext(ctx), rows, normalizedNow)
	if err != nil {
		return err
	}
	return db.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Writer 只落最终 BLOB；任何写入错误仍与本页 checkpoint 一起回滚。
		if err := latencystore.WritePreparedRows(tx, preparedRows); err != nil {
			return err
		}
		// cursor 最后条件推进，冲突时回滚本页已经写入的全部 hour/day rows。
		return AdvanceUsageAggregationCheckpoint(ctx, tx, entities.UsageAggregationCheckpointLatency, expectedCursor, nextCursor, normalizedNow)
	})
}
