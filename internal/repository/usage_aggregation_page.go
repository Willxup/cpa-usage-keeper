package repository

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// usageAggregationEventPageLimit 是共享和 fallback 路径共同使用的最大事件页大小。
	usageAggregationEventPageLimit = 1000
)

// LoadUsageAggregationTargetEventID 用 Reader 固定本轮开始时可见的最大事件 ID。
func LoadUsageAggregationTargetEventID(ctx context.Context, db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	// MAX 只返回一个整数，不加载事件实体；后续页面都受该 target 上界约束。
	var targetEventID int64
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).
		Model(&entities.UsageEvent{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&targetEventID).Error; err != nil {
		return 0, fmt.Errorf("load usage aggregation target event id: %w", err)
	}
	return targetEventID, nil
}

// LoadUsageAggregationEventPage 用固定投影从 Reader 加载 afterID 与 targetID 之间的一页事件。
func LoadUsageAggregationEventPage(ctx context.Context, db *gorm.DB, afterID, targetID int64, limit int) ([]entities.UsageEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	// 已追到目标时直接返回空页，不访问数据库也不启动写事务。
	if targetID <= afterID {
		return []entities.UsageEvent{}, nil
	}
	if afterID < 0 || targetID < 0 {
		return nil, fmt.Errorf("usage aggregation event bounds must be non-negative: after=%d target=%d", afterID, targetID)
	}
	// 非正 limit 属于调用错误；过大 limit 收敛到固定 1000，避免单页无界增长。
	if limit <= 0 {
		return nil, fmt.Errorf("usage aggregation event page limit must be positive")
	}
	if limit > usageAggregationEventPageLimit {
		limit = usageAggregationEventPageLimit
	}
	// 显式 Reader、半开下界、闭合固定目标和 ID 升序共同保证 cursor 可安全取最后一行。
	var events []entities.UsageEvent
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).
		Model(&entities.UsageEvent{}).
		Select(entities.UsageAggregationEventProjectionColumns).
		Where("id > ? AND id <= ?", afterID, targetID).
		Order("id asc").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage aggregation event page: %w", err)
	}
	return events, nil
}
