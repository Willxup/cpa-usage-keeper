package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

const usageAggregationReplayPageLimit = 1000

// LoadUsageAggregationReplayTargetEventID 固定 archive 与 hot 在 migration 启动时的全局最大 ID。
func LoadUsageAggregationReplayTargetEventID(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	var targetID int64
	if err := db.Raw(`
		SELECT MAX(
			COALESCE((SELECT MAX(id) FROM usage_events_archive), 0),
			COALESCE((SELECT MAX(id) FROM usage_events), 0)
		)`).Scan(&targetID).Error; err != nil {
		return 0, fmt.Errorf("load usage event replay target: %w", err)
	}
	return targetID, nil
}

// LoadUsageAggregationReplayEventPage 只供启动 migration 按全局 ID 顺序重放 archive 与 hot 事件。
func LoadUsageAggregationReplayEventPage(db *gorm.DB, afterID, targetID int64, limit int) ([]entities.UsageEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if afterID < 0 || targetID < 0 {
		return nil, fmt.Errorf("usage event replay bounds must be non-negative: after=%d target=%d", afterID, targetID)
	}
	if targetID <= afterID {
		return []entities.UsageEvent{}, nil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("usage event replay page limit must be positive")
	}
	if limit > usageAggregationReplayPageLimit {
		limit = usageAggregationReplayPageLimit
	}

	// 两个分支都按 INTEGER PRIMARY KEY 范围读取，SQLite 会用 MERGE UNION 恢复全局入库顺序。
	columns := entities.UsageEventStorageColumns
	query := fmt.Sprintf(`
		SELECT %s FROM (
			SELECT %s FROM usage_events_archive WHERE id > ? AND id <= ?
			UNION ALL
			SELECT %s FROM usage_events WHERE id > ? AND id <= ?
		) AS usage_event_replay
		ORDER BY id ASC
		LIMIT ?`, columns, columns, columns)
	var events []entities.UsageEvent
	if err := db.Raw(query, afterID, targetID, afterID, targetID, limit).Scan(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage event replay page: %w", err)
	}
	return events, nil
}
