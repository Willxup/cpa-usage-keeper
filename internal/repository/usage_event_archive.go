package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// 每个候选 ID 在单条 IN 查询中占一个绑定变量，复用 repository 的 SQLite 保守变量预算。
const usageEventArchiveBatchSize = sqliteVariableLimit

// ArchiveExpiredUsageEvents 分批把超过 hot 保留线且已完成派生聚合的事件原子移动到冷表。
func ArchiveExpiredUsageEvents(ctx context.Context, db *gorm.DB, now time.Time) (dto.UsageEventArchiveResult, error) {
	if db == nil {
		return dto.UsageEventArchiveResult{}, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := dto.UsageEventArchiveResult{Status: dto.UsageEventArchiveStatusEmpty}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		archived, status, err := archiveExpiredUsageEventsBatch(ctx, db, now)
		if err != nil {
			return result, err
		}
		result.Archived += archived
		if status == dto.UsageEventArchiveStatusAggregationLagging {
			result.Status = status
			return result, nil
		}
		if archived == 0 {
			return result, nil
		}
		result.Status = dto.UsageEventArchiveStatusArchived
	}
}

func archiveExpiredUsageEventsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int64, dto.UsageEventArchiveStatus, error) {
	archived := int64(0)
	status := dto.UsageEventArchiveStatusEmpty
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cutoff := usageEventsArchiveCutoff(now)
		var ids []int64
		if err := tx.Model(&entities.UsageEvent{}).
			Select("id").
			Where("timestamp < ?", timeutil.FormatStorageTime(cutoff)).
			Order("timestamp asc, id asc").
			Limit(usageEventArchiveBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("load usage events archive batch: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}

		// 只有确有过期候选时才检查保守门禁，避免没有归档积压时误报水位阻塞。
		safe, err := usageEventAggregationsCaughtUp(tx)
		if err != nil {
			return err
		}
		if !safe {
			status = dto.UsageEventArchiveStatusAggregationLagging
			return nil
		}

		// INSERT SELECT 避免千万级归档在 Go 内存中反序列化完整事件；列清单是 hot/archive 共享契约。
		columns := entities.UsageEventStorageColumns
		insertSQL := fmt.Sprintf("INSERT INTO usage_events_archive (%s) SELECT %s FROM usage_events WHERE id IN ?", columns, columns)
		insertResult := tx.Exec(insertSQL, ids)
		if insertResult.Error != nil {
			return fmt.Errorf("archive usage events: %w", insertResult.Error)
		}
		if insertResult.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("archive usage events: expected %d inserted rows, got %d", len(ids), insertResult.RowsAffected)
		}

		var archivedCount int64
		if err := tx.Model(&entities.UsageEventArchive{}).Where("id IN ?", ids).Count(&archivedCount).Error; err != nil {
			return fmt.Errorf("verify archived usage events: %w", err)
		}
		if archivedCount != int64(len(ids)) {
			return fmt.Errorf("verify archived usage events: expected %d rows, got %d", len(ids), archivedCount)
		}

		deleteResult := tx.Unscoped().Where("id IN ?", ids).Delete(&entities.UsageEvent{})
		if deleteResult.Error != nil {
			return fmt.Errorf("delete archived usage events from hot table: %w", deleteResult.Error)
		}
		if deleteResult.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("delete archived usage events from hot table: expected %d rows, got %d", len(ids), deleteResult.RowsAffected)
		}
		archived = int64(len(ids))
		status = dto.UsageEventArchiveStatusArchived
		return nil
	})
	if err != nil {
		return 0, status, err
	}
	return archived, status, nil
}

func databaseContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}
