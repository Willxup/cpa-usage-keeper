package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// HasProcessableRedisUsageInbox 只判断是否存在需要前台优先处理的 inbox 行。
func HasProcessableRedisUsageInbox(ctx context.Context, db *gorm.DB) (bool, error) {
	// nil 数据库无法执行 inbox 优先级检查。
	if db == nil {
		return false, fmt.Errorf("database is nil")
	}
	// marker 只承接一行常量，不读取 raw_message 等大字段。
	var marker int
	// pending 和 process_failed 与实际 process runner 的可处理集合保持一致。
	err := db.WithContext(ctx).
		Model(&entities.RedisUsageInbox{}).
		Select("1").
		Where("status = ? OR status = ?", RedisUsageInboxStatusPending, RedisUsageInboxStatusProcessFailed).
		Limit(1).
		Scan(&marker).Error
	// 查询失败时 runner 不得猜测 inbox 为空。
	if err != nil {
		return false, fmt.Errorf("check processable redis usage inbox: %w", err)
	}
	// 读到常量 1 表示前台仍有可处理行，聚合必须立即让路。
	return marker == 1, nil
}

const redisUsageInboxProcessingColumns = "id, source, raw_message, status, attempt_count, usage_event_key, popped_at"

const (
	RedisUsageInboxStatusPending       = "pending"
	RedisUsageInboxStatusProcessed     = "processed"
	RedisUsageInboxStatusDecodeFailed  = "decode_failed"
	RedisUsageInboxStatusProcessFailed = "process_failed"
	RedisUsageInboxStatusDiscarded     = "discarded"
	// RedisUsageInboxSourceUnknown 表示历史或异常写入路径无法可靠还原真实来源。
	RedisUsageInboxSourceUnknown = "unknown"

	redisUsageInboxMaxErrorLength     = 1024
	redisUsageInboxMaxProcessAttempts = 5
	// 每行 CASE 映射占 2 个变量，WHERE IN 再占 1 个；300 行连同固定字段仍低于 SQLite 999 变量限制。
	redisUsageInboxProcessedBatchSize = 300
)

// RedisUsageInboxProcessedUpdate 保存单条 inbox 与最终 usage event 的稳定映射。
type RedisUsageInboxProcessedUpdate struct {
	ID       int64
	EventKey string
}

func InsertRedisUsageInboxRawMessages(db *gorm.DB, source string, messages []string, poppedAt time.Time) ([]entities.RedisUsageInbox, error) {
	// inputs 统一走结构化 DTO，避免 raw message 快捷入口和测试入口出现两套入库逻辑。
	inputs := make([]dto.RedisInboxInsert, 0, len(messages))
	for _, message := range messages {
		// source 原样传给标准入口，真正的 trim/兜底只在一个地方完成。
		inputs = append(inputs, dto.RedisInboxInsert{Source: source, RawMessage: message, PoppedAt: poppedAt})
	}
	// InsertRedisUsageInboxMessages 是唯一实际批量写入入口。
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
			Source:       redisUsageInboxSource(input.Source),
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

func redisUsageInboxSource(value string) string {
	// 统一去掉配置或调用方传入来源名两端空白，避免同一来源出现多个字符串形态。
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		// 空来源不能写入非空列，也不能伪造成某个真实拉取方式。
		return RedisUsageInboxSourceUnknown
	}
	// 非空来源保持调用方完整语义，例如 redis_subscribe:usage 或 redis_pull:queue。
	return trimmed
}

func MarkRedisUsageInboxProcessed(db *gorm.DB, id int64, eventKey string, processedAt time.Time) error {
	return db.Model(&entities.RedisUsageInbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":          RedisUsageInboxStatusProcessed,
		"usage_event_key": eventKey,
		"processed_at":    timeutil.FormatStorageTime(processedAt),
		"last_error":      "",
	}).Error
}

// MarkRedisUsageInboxProcessedBatch 分批更新 processed 状态，同时保持每个 inbox ID 对应自己的 event key。
func MarkRedisUsageInboxProcessedBatch(db *gorm.DB, updates []RedisUsageInboxProcessedUpdate, processedAt time.Time) error {
	for start := 0; start < len(updates); start += redisUsageInboxProcessedBatchSize {
		end := min(start+redisUsageInboxProcessedBatchSize, len(updates))
		batch := updates[start:end]
		ids := make([]int64, 0, len(batch))
		caseArgs := make([]any, 0, len(batch)*2)
		var eventKeyCase strings.Builder
		eventKeyCase.WriteString("CASE id")
		for _, update := range batch {
			eventKeyCase.WriteString(" WHEN ? THEN ?")
			caseArgs = append(caseArgs, update.ID, update.EventKey)
			ids = append(ids, update.ID)
		}
		eventKeyCase.WriteString(" ELSE usage_event_key END")

		result := db.Model(&entities.RedisUsageInbox{}).Where("id IN ?", ids).Updates(map[string]any{
			"status":          RedisUsageInboxStatusProcessed,
			"usage_event_key": gorm.Expr(eventKeyCase.String(), caseArgs...),
			"processed_at":    timeutil.FormatStorageTime(processedAt),
			"last_error":      "",
		})
		if result.Error != nil {
			return fmt.Errorf("mark redis usage inbox processed batch [%d:%d]: %w", start, end, result.Error)
		}
	}
	return nil
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
