package repository

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

const (
	RedisUsageInboxStatusPending       = "pending"
	RedisUsageInboxStatusProcessed     = "processed"
	RedisUsageInboxStatusDecodeFailed  = "decode_failed"
	RedisUsageInboxStatusProcessFailed = "process_failed"
	RedisUsageInboxStatusDiscarded     = "discarded"

	redisUsageInboxMaxErrorLength     = 1024
	redisUsageInboxMaxProcessAttempts = 5
)

type RedisInboxInsert struct {
	QueueKey   string
	RawMessage string
	PoppedAt   time.Time
}

func InsertRedisUsageInboxMessages(db *gorm.DB, inputs []RedisInboxInsert) ([]models.RedisUsageInbox, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	rows := make([]models.RedisUsageInbox, 0, len(inputs))
	for _, input := range inputs {
		hash := sha256.Sum256([]byte(input.RawMessage))
		rows = append(rows, models.RedisUsageInbox{
			QueueKey:     strings.TrimSpace(input.QueueKey),
			MessageHash:  fmt.Sprintf("%x", hash),
			RawMessage:   input.RawMessage,
			Status:       RedisUsageInboxStatusPending,
			AttemptCount: 0,
			PoppedAt:     input.PoppedAt.UTC(),
		})
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Create(&rows).Error
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

func MarkRedisUsageInboxProcessed(db *gorm.DB, id uint, snapshotRunID uint, eventKey string, processedAt time.Time) error {
	return db.Model(&models.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":          RedisUsageInboxStatusProcessed,
		"snapshot_run_id": snapshotRunID,
		"usage_event_key": eventKey,
		"processed_at":    processedAt.UTC(),
		"last_error":      "",
	}).Error
}

func MarkRedisUsageInboxDecodeFailed(db *gorm.DB, id uint, decodeErr error) error {
	return markRedisUsageInboxFailed(db, id, RedisUsageInboxStatusDecodeFailed, decodeErr)
}

// MarkRedisUsageInboxProcessFailed 最多保留 5 次处理重试，超过后丢弃，避免旧数据长期阻塞新数据。
func MarkRedisUsageInboxProcessFailed(db *gorm.DB, id uint, processErr error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var row models.RedisUsageInbox
		if err := tx.First(&row, id).Error; err != nil {
			return err
		}
		nextAttempts := row.AttemptCount + 1
		status := RedisUsageInboxStatusProcessFailed
		if nextAttempts >= redisUsageInboxMaxProcessAttempts {
			status = RedisUsageInboxStatusDiscarded
		}
		return tx.Model(&models.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
			"status":        status,
			"attempt_count": nextAttempts,
			"last_error":    boundedRedisUsageInboxError(processErr),
		}).Error
	})
}

// ListProcessableRedisUsageInbox 返回待处理和可重试的数据，不返回已解码失败或已丢弃的数据。
func ListProcessableRedisUsageInbox(db *gorm.DB, limit int) ([]models.RedisUsageInbox, error) {
	query := db.Where("status = ? OR (status = ? AND attempt_count < ?)", RedisUsageInboxStatusPending, RedisUsageInboxStatusProcessFailed, redisUsageInboxMaxProcessAttempts).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []models.RedisUsageInbox
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func ListPendingRedisUsageInbox(db *gorm.DB, limit int) ([]models.RedisUsageInbox, error) {
	query := db.Where("status = ?", RedisUsageInboxStatusPending).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []models.RedisUsageInbox
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func markRedisUsageInboxFailed(db *gorm.DB, id uint, status string, err error) error {
	return db.Model(&models.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
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
