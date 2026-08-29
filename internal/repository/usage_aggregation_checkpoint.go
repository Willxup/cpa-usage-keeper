package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"
)

// UsageAggregationCheckpointSnapshot 是一次 SELECT 得到的三个已提交全局水位快照。
type UsageAggregationCheckpointSnapshot struct {
	// OverviewCursor 只代表 Overview 独立事务已经提交的进度。
	OverviewCursor int64
	// ActivityCursor 只代表 Activity 独立事务已经提交的进度。
	ActivityCursor int64
	// LatencyCursor 只代表 Latency 独立事务已经提交的进度。
	LatencyCursor int64
}

// Equal 只比较三个 cursor；时间戳不参与共享快路径判断。
func (snapshot UsageAggregationCheckpointSnapshot) Equal() bool {
	return snapshot.OverviewCursor == snapshot.ActivityCursor && snapshot.ActivityCursor == snapshot.LatencyCursor
}

// LoadUsageAggregationCheckpointSnapshot 用一次 Reader 查询读取三个水位，缺行保持零值。
func LoadUsageAggregationCheckpointSnapshot(ctx context.Context, db *gorm.DB) (UsageAggregationCheckpointSnapshot, error) {
	if db == nil {
		return UsageAggregationCheckpointSnapshot{}, fmt.Errorf("database is nil")
	}
	// 固定名称集合防止调用方遗漏某一水位，也让 SQL 始终保持单次 IN 查询。
	names := []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	}
	// 只读路径不得 FirstOrCreate；缺失行由结构体零值表达首次聚合状态。
	var checkpoints []entities.UsageAggregationCheckpoint
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).
		Where("name IN ?", names).
		Find(&checkpoints).Error; err != nil {
		return UsageAggregationCheckpointSnapshot{}, fmt.Errorf("load usage aggregation checkpoints: %w", err)
	}
	// 每行按受限 name 映射到固定字段，数据库中的未知值不影响其它合法水位。
	snapshot := UsageAggregationCheckpointSnapshot{}
	for _, checkpoint := range checkpoints {
		switch checkpoint.Name {
		case entities.UsageAggregationCheckpointOverview:
			snapshot.OverviewCursor = checkpoint.LastAggregatedUsageEventID
		case entities.UsageAggregationCheckpointActivity:
			snapshot.ActivityCursor = checkpoint.LastAggregatedUsageEventID
		case entities.UsageAggregationCheckpointLatency:
			snapshot.LatencyCursor = checkpoint.LastAggregatedUsageEventID
		}
	}
	return snapshot, nil
}

// AdvanceUsageAggregationCheckpoint 在调用方事务内按 expected cursor 条件推进一行。
func AdvanceUsageAggregationCheckpoint(ctx context.Context, tx *gorm.DB, name entities.UsageAggregationCheckpointName, expectedCursor, nextCursor int64, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("database is nil")
	}
	// 只允许实体层声明的三种名称，避免 typo 静默创建第四类 checkpoint。
	switch name {
	case entities.UsageAggregationCheckpointOverview, entities.UsageAggregationCheckpointActivity, entities.UsageAggregationCheckpointLatency:
	default:
		return fmt.Errorf("unsupported usage aggregation checkpoint %q", name)
	}
	// cursor 必须单调前进；相等更新会伪造 StatsUpdatedAt，新值更小则会重复累计。
	if expectedCursor < 0 || nextCursor <= expectedCursor {
		return fmt.Errorf("invalid usage aggregation checkpoint advance %q: %d -> %d", name, expectedCursor, nextCursor)
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 首次推进时在同一事务内补零水位行；已有行由 ON CONFLICT 保持不变。
	checkpoint := entities.UsageAggregationCheckpoint{
		Name:                       name,
		LastAggregatedUsageEventID: 0,
		CreatedAt:                  normalizedNow,
		UpdatedAt:                  normalizedNow,
	}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoint).Error; err != nil {
		return fmt.Errorf("ensure usage aggregation checkpoint %q: %w", name, err)
	}
	// name + expectedCursor 是唯一并发保护；rows 与 cursor 由外层事务共同提交或回滚。
	result := tx.WithContext(ctx).Model(&entities.UsageAggregationCheckpoint{}).
		Where("name = ? AND last_aggregated_usage_event_id = ?", name, expectedCursor).
		Updates(map[string]any{
			"last_aggregated_usage_event_id": nextCursor,
			"stats_updated_at":               timeutil.FormatStorageTime(normalizedNow),
			"updated_at":                     timeutil.FormatStorageTime(normalizedNow),
		})
	if result.Error != nil {
		return fmt.Errorf("advance usage aggregation checkpoint %q: %w", name, result.Error)
	}
	// 没有恰好更新一行表示调用方基于过期快照，必须让外层事务回滚 rows。
	if result.RowsAffected != 1 {
		return fmt.Errorf("usage aggregation checkpoint %q conflict: expected cursor %d", name, expectedCursor)
	}
	return nil
}
