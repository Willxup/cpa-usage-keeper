package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// InsertPricingBootstrapInboxRawMessages 在每次启动业务就绪前保存 CPA 原始消息。
// 每批在唯一 writer 事务内识别实际旧/新列并插入，历史 DDL 不会插在列检查与写入之间。
// 正常业务接收继续使用 InsertRedisUsageInboxRawMessages；这里不解码、不去重、不消费事件。
func InsertPricingBootstrapInboxRawMessages(ctx context.Context, db *gorm.DB, source string, messages []string, receivedAt time.Time) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	source = redisUsageInboxSource(source)
	when := timeutil.FormatStorageTime(receivedAt)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasTable("redis_usage_inboxes") {
			return fmt.Errorf("pricing bootstrap inbox is missing")
		}
		hasSource := tx.Migrator().HasColumn("redis_usage_inboxes", "source")
		hasQueueKey := tx.Migrator().HasColumn("redis_usage_inboxes", "queue_key")
		if !hasSource && !hasQueueKey {
			return fmt.Errorf("pricing bootstrap inbox has no source column")
		}
		// 单条 SQL 最多 100 行，八列时低于 SQLite 999 参数上限；整批消息仍共用当前事务。
		for start := 0; start < len(messages); start += 100 {
			end := min(start+100, len(messages))
			rows := make([]map[string]any, 0, end-start)
			for _, raw := range messages[start:end] {
				hash := sha256.Sum256([]byte(raw))
				row := map[string]any{
					"message_hash": fmt.Sprintf("%x", hash), "raw_message": raw,
					"status": RedisUsageInboxStatusPending, "popped_at": when,
					"created_at": when, "updated_at": when,
				}
				if hasSource {
					row["source"] = source
				}
				if hasQueueKey {
					// 历史 queue_key 只能保存 CPA 队列键，转换后仍由旧 migration 统一写 unknown 来源。
					key := "usage"
					if strings.HasSuffix(source, ":queue") {
						key = "queue"
					}
					row["queue_key"] = key
				}
				rows = append(rows, row)
			}
			if err := tx.Table("redis_usage_inboxes").CreateInBatches(&rows, len(rows)).Error; err != nil {
				return fmt.Errorf("insert pricing bootstrap inbox messages: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}
