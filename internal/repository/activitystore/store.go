package activitystore

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// ApplyRows 用统一 upsert 语义把内存 Activity rows 累加到 SQLite。
func ApplyRows(tx *gorm.DB, rows []entities.UsageActivityStat, now time.Time) error {
	// migration 和运行时都逐行调用同一个实现，字段集合不能分叉。
	for _, row := range rows {
		// 任一 row 写入失败都交给调用方事务整体回滚。
		if err := applyRow(tx, row, now); err != nil {
			return err
		}
	}
	// 所有 rows 写入成功后返回，由调用方在同一事务推进自己的 checkpoint。
	return nil
}

func applyRow(tx *gorm.DB, row entities.UsageActivityStat, now time.Time) error {
	// 先按最终唯一键尝试增量 UPDATE，避免依赖 SQLite 专属 upsert 语法。
	updates := map[string]any{
		// bucket_end 始终刷新为统一 helper 计算出的真实终点。
		"bucket_end": timeutil.FormatSortableStorageTime(row.BucketEnd),
		// 成功请求数按当前 batch 增量累加。
		"success_count": gorm.Expr("success_count + ?", row.SuccessCount),
		// 失败请求数按当前 batch 增量累加。
		"failure_count": gorm.Expr("failure_count + ?", row.FailureCount),
		// canonical input_tokens 按当前 batch 增量累加。
		"input_tokens": gorm.Expr("input_tokens + ?", row.InputTokens),
		// canonical output_tokens 按当前 batch 增量累加。
		"output_tokens": gorm.Expr("output_tokens + ?", row.OutputTokens),
		// canonical reasoning_tokens 按当前 batch 增量累加。
		"reasoning_tokens": gorm.Expr("reasoning_tokens + ?", row.ReasoningTokens),
		// canonical cache_read_tokens 按当前 batch 增量累加。
		"cache_read_tokens": gorm.Expr("cache_read_tokens + ?", row.CacheReadTokens),
		// canonical cache_creation_tokens 按当前 batch 增量累加。
		"cache_creation_tokens": gorm.Expr("cache_creation_tokens + ?", row.CacheCreationTokens),
		// canonical total_tokens 按当前 batch 增量累加。
		"total_tokens": gorm.Expr("total_tokens + ?", row.TotalTokens),
		// updated_at 使用本次聚合固定 now。
		"updated_at": timeutil.FormatStorageTime(now),
	}
	// sortableTime serializer 落库为固定 UTC 字符串，因此唯一键查询也使用同一格式。
	result := tx.Model(&entities.UsageActivityStat{}).
		Where("grain = ? AND bucket_start = ? AND api_group_key = ?", row.Grain, timeutil.FormatSortableStorageTime(row.BucketStart), row.APIGroupKey).
		Updates(updates)
	// UPDATE 失败时禁止继续 INSERT，避免掩盖数据库错误。
	if result.Error != nil {
		return fmt.Errorf("update usage activity row: %w", result.Error)
	}
	// 已有行累计成功后当前 row 已完成。
	if result.RowsAffected > 0 {
		return nil
	}

	// 新 key 首次出现时显式记录统一聚合时间。
	row.CreatedAt = timeutil.NormalizeStorageTime(now)
	row.UpdatedAt = timeutil.NormalizeStorageTime(now)
	// 任何约束或 trigger 错误都返回给上层事务。
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("insert usage activity row: %w", err)
	}
	// 新行创建成功，当前 row 的累计完成。
	return nil
}
