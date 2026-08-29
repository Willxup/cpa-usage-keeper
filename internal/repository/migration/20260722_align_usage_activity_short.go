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

const usageActivityShortAlignmentBatchSize = 1000

// alignUsageActivityShortMigration 从 raw events 原子重建仍在保留期内的 short 行。
func alignUsageActivityShortMigration(tx *gorm.DB) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	retention, limited := activity.Retention(entities.UsageActivityGrainShort)
	if !limited {
		return fmt.Errorf("usage activity short retention is missing")
	}
	// 只重建 checkpoint 已确认完成的事件；其后的事件仍由正常增量统一处理四种 grain。
	var checkpoint entities.UsageActivityAggregationCheckpoint
	if err := tx.Where("name = ?", usageActivityCheckpointName).Take(&checkpoint).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load usage activity short rebuild checkpoint: %w", err)
	}
	// 先删除旧全局边界；外层 migration 事务保证失败时完整恢复旧行。
	if err := tx.Where("grain = ?", entities.UsageActivityGrainShort).Delete(&entities.UsageActivityStat{}).Error; err != nil {
		return fmt.Errorf("delete legacy usage activity short rows: %w", err)
	}

	cutoff := now.Add(-retention)
	var lastEventID int64
	for {
		// 只读取 short 所需字段，并用 ID 游标限制单批内存占用。
		var events []entities.UsageEvent
		if err := tx.Select("id, api_group_key, timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
			Where("id > ? AND id <= ? AND timestamp >= ?", lastEventID, checkpoint.LastAggregatedUsageEventID, timeutil.FormatStorageTime(cutoff)).
			Order("id asc").
			Limit(usageActivityShortAlignmentBatchSize).
			Find(&events).Error; err != nil {
			return fmt.Errorf("load usage activity short rebuild events: %w", err)
		}
		if len(events) == 0 {
			return nil
		}

		rows, err := activity.BuildRows(events, now)
		if err != nil {
			return err
		}
		shortRows := rows[:0]
		for _, row := range rows {
			if row.Grain == entities.UsageActivityGrainShort {
				shortRows = append(shortRows, row)
			}
		}
		if err := activitystore.ApplyRows(tx, shortRows, now); err != nil {
			return err
		}
		lastEventID = events[len(events)-1].ID
	}
}
