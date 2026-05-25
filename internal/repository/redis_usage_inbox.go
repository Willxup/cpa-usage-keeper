package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const redisUsageInboxProcessingColumns = "id, queue_key, raw_message, status, attempt_count, usage_event_key, popped_at"

const (
	RedisUsageInboxStatusPending       = "pending"
	RedisUsageInboxStatusProcessed     = "processed"
	RedisUsageInboxStatusDecodeFailed  = "decode_failed"
	RedisUsageInboxStatusProcessFailed = "process_failed"
	RedisUsageInboxStatusDiscarded     = "discarded"

	redisUsageInboxMaxErrorLength     = 1024
	redisUsageInboxMaxProcessAttempts = 5
)

func InsertRedisUsageInboxRawMessages(db *gorm.DB, queueKey string, messages []string, poppedAt time.Time) ([]entities.RedisUsageInbox, error) {
	inputs := make([]dto.RedisInboxInsert, 0, len(messages))
	for _, message := range messages {
		inputs = append(inputs, dto.RedisInboxInsert{QueueKey: queueKey, RawMessage: message, PoppedAt: poppedAt})
	}
	return InsertRedisUsageInboxMessages(db, inputs)
}

func InsertRedisUsageInboxMessages(db *gorm.DB, inputs []dto.RedisInboxInsert) ([]entities.RedisUsageInbox, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	rows := make([]entities.RedisUsageInbox, 0, len(inputs))
	// 先把 Redis 原始消息转换成 inbox 行，后续落库只处理标准化后的模型数据。
	for _, input := range inputs {
		hash := sha256.Sum256([]byte(input.RawMessage))
		rows = append(rows, entities.RedisUsageInbox{
			QueueKey:     strings.TrimSpace(input.QueueKey),
			MessageHash:  fmt.Sprintf("%x", hash),
			RawMessage:   input.RawMessage,
			Status:       RedisUsageInboxStatusPending,
			AttemptCount: 0,
			PoppedAt:     timeutil.NormalizeStorageTime(input.PoppedAt),
		})
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		// Redis 拉取批次仍由配置控制；这里只把数据库写入拆成安全大小。
		return tx.CreateInBatches(&rows, insertBatchSize(entities.RedisUsageInbox{})).Error
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

func MarkRedisUsageInboxProcessed(db *gorm.DB, id int64, eventKey string, processedAt time.Time) error {
	return db.Model(&entities.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":          RedisUsageInboxStatusProcessed,
		"usage_event_key": eventKey,
		"processed_at":    timeutil.FormatStorageTime(processedAt),
		"last_error":      "",
	}).Error
}

func MarkRedisUsageInboxDecodeFailed(db *gorm.DB, id int64, decodeErr error) error {
	return markRedisUsageInboxFailed(db, id, RedisUsageInboxStatusDecodeFailed, decodeErr)
}

func MarkRedisUsageInboxProcessFailed(db *gorm.DB, id int64, processErr error) error {
	return db.Model(&entities.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
		"status": gorm.Expr(
			"CASE WHEN attempt_count + ? >= ? THEN ? ELSE ? END",
			1,
			redisUsageInboxMaxProcessAttempts,
			RedisUsageInboxStatusDiscarded,
			RedisUsageInboxStatusProcessFailed,
		),
		"attempt_count": gorm.Expr("attempt_count + ?", 1),
		"last_error":    boundedRedisUsageInboxError(processErr),
	}).Error
}

// ListProcessableRedisUsageInbox 返回待处理和可重试的数据，不返回已解码失败或已丢弃的数据。
func ListProcessableRedisUsageInbox(db *gorm.DB, limit int) ([]entities.RedisUsageInbox, error) {
	query := db.Select(redisUsageInboxProcessingColumns).Where("status = ? OR status = ?", RedisUsageInboxStatusPending, RedisUsageInboxStatusProcessFailed).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []entities.RedisUsageInbox
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func ListPendingRedisUsageInbox(db *gorm.DB, limit int) ([]entities.RedisUsageInbox, error) {
	query := db.Select(redisUsageInboxProcessingColumns).Where("status = ?", RedisUsageInboxStatusPending).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []entities.RedisUsageInbox
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// CleanupRedisUsageInbox 清理已完成和失败的 Redis inbox 原始消息，pending 数据永远不在这里删除。
// processed 保留到下一个本地日开始后才清理；decode_failed/process_failed/discarded 保留 7 天便于排查。
func CleanupRedisUsageInbox(db *gorm.DB, now time.Time) (dto.RedisUsageInboxCleanupResult, error) {
	localNow := now.In(time.Local)
	localDayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	processedCutoff := timeutil.FormatStorageTime(localDayStart)
	failedCutoff := timeutil.FormatStorageTime(now.AddDate(0, 0, -7))
	result := dto.RedisUsageInboxCleanupResult{}

	processedDelete := db.Where("status = ? AND processed_at IS NOT NULL AND processed_at < ?", RedisUsageInboxStatusProcessed, processedCutoff).Delete(&entities.RedisUsageInbox{})
	if processedDelete.Error != nil {
		return result, processedDelete.Error
	}
	result.ProcessedDeleted = processedDelete.RowsAffected

	failedDelete := db.Where("status IN ? AND updated_at < ?", []string{RedisUsageInboxStatusDecodeFailed, RedisUsageInboxStatusProcessFailed, RedisUsageInboxStatusDiscarded}, failedCutoff).Delete(&entities.RedisUsageInbox{})
	if failedDelete.Error != nil {
		return result, failedDelete.Error
	}
	result.FailedDeleted = failedDelete.RowsAffected

	return result, nil
}

func markRedisUsageInboxFailed(db *gorm.DB, id int64, status string, err error) error {
	return db.Model(&entities.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":        status,
		"attempt_count": gorm.Expr("attempt_count + ?", 1),
		"last_error":    boundedRedisUsageInboxError(err),
	}).Error
}

func boundedRedisUsageInboxError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) <= redisUsageInboxMaxErrorLength {
		return message
	}
	message = message[:redisUsageInboxMaxErrorLength]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}
