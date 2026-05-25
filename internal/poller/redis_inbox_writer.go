package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type RepositoryRedisInboxWriter struct {
	// db 是 redis_usage_inboxes 的落库连接。
	db *gorm.DB
	// queueKey 保留配置中的 Redis queue key，不能写成 source 标签，避免改变历史数据语义。
	queueKey string
}

func NewRedisInboxWriter(db *gorm.DB, queueKey string) *RepositoryRedisInboxWriter {
	// queue key 允许配置中带空白，这里统一 trim 后保存。
	trimmed := strings.TrimSpace(queueKey)
	if trimmed == "" {
		// 配置缺省时沿用 CPA 旧 Redis usage queue key。
		trimmed = cpa.ManagementUsageQueueKey
	}
	// writer 只保存依赖，不主动访问数据库。
	return &RepositoryRedisInboxWriter{db: db, queueKey: trimmed}
}

func (w *RepositoryRedisInboxWriter) Insert(ctx context.Context, _ string, messages []string, receivedAt time.Time) (int, error) {
	if len(messages) == 0 {
		// 空批次不落库，避免无意义事务和 updated_at 变化。
		return 0, nil
	}
	if w == nil || w.db == nil {
		// 数据库缺失是构造错误，必须显式返回。
		return 0, fmt.Errorf("redis inbox writer database is nil")
	}
	if err := ctx.Err(); err != nil {
		// 调用方已取消时不再写数据库。
		return 0, err
	}
	// 所有来源都写入同一个 queueKey，source 只用于 runner 日志/测试，不改变 inbox 语义。
	rows, err := repository.InsertRedisUsageInboxRawMessages(w.db.WithContext(ctx), w.queueKey, messages, receivedAt)
	if err != nil {
		// 插入失败交给 runner 记录 error 并进入对应失败路径。
		return 0, err
	}
	// 返回实际插入行数，供状态机判断是否有数据。
	return len(rows), nil
}
