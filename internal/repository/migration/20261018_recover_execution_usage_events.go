package migration

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	executionEventRecoveryBatchSize = 500
	// 查询和 UPDATE IN 共用此专用上限，连同状态与更新字段的参数仍小于 SQLite 的 999 限制。
	executionInboxRecoveryBatchSize = 500
	executionRecoveryOldDecodeError = "decode redis usage message: request_id is required"
)

func recoverExecutionUsageEventsMigration(db *gorm.DB) error {
	// 表名为固定白名单；范围参数走绑定，不能对无 event_key 索引的空 key 反复从头分页。
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := recoverExecutionEventKeys(db, table); err != nil {
			return err
		}
	}
	return recoverExecutionInbox(db)
}

func recoverExecutionEventKeys(db *gorm.DB, table string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	startedAt := time.Now()
	var upperBound int64
	if err := db.Table(table).Select("COALESCE(MAX(id), 0)").Scan(&upperBound).Error; err != nil {
		return fmt.Errorf("load %s execution recovery upper bound: %w", table, err)
	}
	var cursor, scanned, modified int64
	var batches int
	var maxBatchDuration time.Duration
	for cursor < upperBound {
		var ids []int64
		if err := db.Table(table).Where("id > ? AND id <= ?", cursor, upperBound).Order("id ASC").Limit(executionEventRecoveryBatchSize).Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("load %s execution recovery IDs: %w", table, err)
		}
		if len(ids) == 0 {
			break
		}
		lastID := ids[len(ids)-1]
		batchStartedAt := time.Now()
		var changed int64
		// 只在当前连续主键范围内更新目标列；失败不推进 cursor，重启可安全从头遍历。
		if err := db.Transaction(func(tx *gorm.DB) error {
			result := tx.Table(table).
				Where("id > ? AND id <= ?", cursor, lastID).
				Where("(event_key IS NULL OR event_key = ?) AND execution_id IS NOT NULL AND execution_id <> ?", "", "").
				UpdateColumn("event_key", gorm.Expr("execution_id"))
			changed = result.RowsAffected
			return result.Error
		}); err != nil {
			return fmt.Errorf("recover %s execution event keys: %w", table, err)
		}
		maxBatchDuration = max(maxBatchDuration, time.Since(batchStartedAt))
		cursor = lastID
		scanned += int64(len(ids))
		modified += changed
		batches++
	}
	logrus.WithFields(logrus.Fields{"table": table, "scanned_rows": scanned, "candidate_rows": modified, "modified_rows": modified, "batches": batches, "duration": time.Since(startedAt), "max_batch_duration": maxBatchDuration}).Info("execution event key recovery completed")
	return nil
}

func recoverExecutionInbox(db *gorm.DB) error {
	const table = "redis_usage_inboxes"
	if !db.Migrator().HasTable(table) {
		return nil
	}
	startedAt := time.Now()
	var upperBound int64
	if err := db.Table(table).Select("COALESCE(MAX(id), 0)").Scan(&upperBound).Error; err != nil {
		return fmt.Errorf("load execution inbox recovery upper bound: %w", err)
	}
	var cursor, candidates, eligible, modified int64
	var batches int
	var maxBatchDuration time.Duration
	for cursor < upperBound {
		var rows []struct {
			ID         int64
			RawMessage string
		}
		if err := db.Table(table).Select("id, raw_message").
			Where("status = ? AND last_error = ? AND id > ? AND id <= ?", "decode_failed", executionRecoveryOldDecodeError, cursor, upperBound).
			Order("id ASC").Limit(executionInboxRecoveryBatchSize).Find(&rows).Error; err != nil {
			return fmt.Errorf("load execution inbox recovery candidates: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			// 只解码恢复条件需要的 ID；其余完整字段仍由正常消费入口验证。
			var payload struct {
				RequestID   string  `json:"request_id"`
				ExecutionID *string `json:"execution_id"`
			}
			if err := json.Unmarshal([]byte(row.RawMessage), &payload); err == nil && strings.TrimSpace(payload.RequestID) == "" && payload.ExecutionID != nil && *payload.ExecutionID != "" {
				ids = append(ids, row.ID)
			}
		}
		if len(ids) > 0 {
			batchStartedAt := time.Now()
			var changed int64
			if err := db.Transaction(func(tx *gorm.DB) error {
				// UpdateColumns 避开 GORM 自动更新时间，避免延长原始失败消息的保留期。
				result := tx.Table(table).Where("id IN ? AND status = ? AND last_error = ?", ids, "decode_failed", executionRecoveryOldDecodeError).
					UpdateColumns(map[string]any{"status": "pending", "last_error": "", "attempt_count": 0})
				changed = result.RowsAffected
				return result.Error
			}); err != nil {
				return fmt.Errorf("recover execution inbox candidates: %w", err)
			}
			maxBatchDuration = max(maxBatchDuration, time.Since(batchStartedAt))
			modified += changed
		}
		cursor = rows[len(rows)-1].ID
		candidates += int64(len(rows))
		eligible += int64(len(ids))
		batches++
	}
	logrus.WithFields(logrus.Fields{"table": table, "candidate_rows": candidates, "eligible_rows": eligible, "modified_rows": modified, "batches": batches, "duration": time.Since(startedAt), "max_batch_duration": maxBatchDuration}).Info("execution inbox recovery completed")
	return nil
}
