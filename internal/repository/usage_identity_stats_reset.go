package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func ResetUsageIdentityStats(ctx context.Context, db *gorm.DB, id int64, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if now.IsZero() {
		return fmt.Errorf("usage identity stats reset time is zero")
	}
	// 所有基线从同一条 UPDATE 的当前行取值，与聚合写入串行；不读取请求明细或改变游标。
	result := db.WithContext(ctx).Model(&entities.UsageIdentity{}).Where("id = ?", id).UpdateColumns(map[string]any{
		"stats_reset_at":          timeutil.FormatStorageTime(now),
		"reset_total_requests":    gorm.Expr("COALESCE(total_requests, 0)"),
		"reset_success_count":     gorm.Expr("COALESCE(success_count, 0)"),
		"reset_failure_count":     gorm.Expr("COALESCE(failure_count, 0)"),
		"reset_input_tokens":      gorm.Expr("COALESCE(input_tokens, 0)"),
		"reset_cache_read_tokens": gorm.Expr("COALESCE(cache_read_tokens, 0)"),
		"reset_total_tokens":      gorm.Expr("COALESCE(total_tokens, 0)"),
	})
	if result.Error != nil {
		return fmt.Errorf("reset usage identity stats: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
