package poller

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type RepositoryRedisInboxWriter struct {
	// db 是 redis_usage_inboxes 的落库连接。
	db *gorm.DB
}

// PricingBootstrapInboxWriter 仅供首次升级接收使用，按物理 inbox 列写原始消息。
// 业务就绪后必须改用标准 RepositoryRedisInboxWriter，不把旧 queue_key 路径带到日常处理。
type PricingBootstrapInboxWriter struct {
	db *gorm.DB
}

func NewPricingBootstrapInboxWriter(db *gorm.DB) *PricingBootstrapInboxWriter {
	return &PricingBootstrapInboxWriter{db: db}
}

// Insert 只持久化原消息；列切换与历史 migration 使用同一个 writer 池串行执行。
func (w *PricingBootstrapInboxWriter) Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}
	if w == nil || w.db == nil {
		return 0, fmt.Errorf("pricing bootstrap inbox writer database is nil")
	}
	return repository.InsertPricingBootstrapInboxRawMessages(ctx, w.db, source, messages, receivedAt)
}

func NewRedisInboxWriter(db *gorm.DB) *RepositoryRedisInboxWriter {
	// writer 只保存依赖，不主动访问数据库。
	return &RepositoryRedisInboxWriter{db: db}
}

func (w *RepositoryRedisInboxWriter) Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error) {
	// source 是最终写入 redis_usage_inboxes.source 的完整来源名，不再表示 Redis queue key。
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
	// 来源名由 runner 传入，完整落库便于区分 subscribe、redis pull 和 HTTP pull。
	rows, err := repository.InsertRedisUsageInboxRawMessages(w.db.WithContext(ctx), source, messages, receivedAt)
	if err != nil {
		// 插入失败交给 runner 记录 error 并进入对应失败路径。
		return 0, err
	}
	// 返回实际插入行数，供状态机判断是否有数据。
	return len(rows), nil
}

type ControlAwareRedisInboxWriter struct {
	// delegate 继续负责普通 usage raw message 的持久化。
	delegate RedisInboxWriter
	// observer 接收控制消息，不参与普通 usage 落库。
	observer RedisControlMessageObserver
}

func NewControlAwareRedisInboxWriter(delegate RedisInboxWriter, observer RedisControlMessageObserver) *ControlAwareRedisInboxWriter {
	// wrapper 只组合依赖，不主动访问数据库或启动同步。
	return &ControlAwareRedisInboxWriter{delegate: delegate, observer: observer}
}

func (w *ControlAwareRedisInboxWriter) Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error) {
	// delegate 缺失会导致普通 usage 无法落库，必须显式失败。
	if w == nil || w.delegate == nil {
		return 0, fmt.Errorf("redis inbox writer delegate is nil")
	}
	if len(messages) == 0 {
		// 空批次保持 no-op，不把空 slice 继续委托给底层 writer。
		return 0, nil
	}

	// usageMessages 只在遇到需过滤消息时创建，usage-only 热路径直接委托原 slice。
	var usageMessages []string
	filtered := false
	for i, message := range messages {
		// 空 payload/null 不是 usage，也不是控制通知；统一在 writer 层丢弃。
		if isIgnorableRawPayload(message) {
			if !filtered {
				// 首次过滤时复制前面的 usage 前缀，后续只 append 保留项。
				usageMessages = cloneUsagePrefix(messages, i)
				filtered = true
			}
			continue
		}
		// 每条 raw message 先做轻量分类，再决定是通知还是落库。
		control := ClassifyRedisControlMessage(message)
		// 普通 usage、非法 JSON、未知结构都保持 passthrough，避免误丢数据。
		if !control.IsControl {
			if filtered {
				usageMessages = append(usageMessages, message)
			}
			continue
		}

		if !filtered {
			// 控制消息也需要从落库批次里移除，因此在这里启动过滤 slice。
			usageMessages = cloneUsagePrefix(messages, i)
			filtered = true
		}
		// 控制消息只驱动 metadata 调度状态，不进入 redis_usage_inboxes。
		if w.observer != nil && control.SupportRefresh {
			// support_refresh=true 让 metadata runner 进入通知模式。
			w.observer.MarkRefreshSupported()
		}
		if w.observer != nil && control.Refresh {
			// refresh=true 触发 metadata runner 的 debounce 同步请求。
			w.observer.RequestMetadataRefresh()
		}
	}
	if !filtered {
		// usage-only 批次保持原 slice，减少高吞吐路径上的分配和 string header 拷贝。
		return w.delegate.Insert(ctx, source, messages, receivedAt)
	}
	// 整批都是控制消息时不调用底层 writer，避免空事务。
	if len(usageMessages) == 0 {
		return 0, nil
	}

	// 非控制消息原样委托给原 writer，保持三种 usage 获取路径的落库语义。
	return w.delegate.Insert(ctx, source, usageMessages, receivedAt)
}

func cloneUsagePrefix(messages []string, end int) []string {
	// 预留原批次容量，避免后续保留消息 append 时多次扩容。
	usageMessages := make([]string, end, len(messages))
	copy(usageMessages, messages[:end])
	return usageMessages
}

func isIgnorableRawPayload(raw string) bool {
	// 只跳过 JSON 标准空白，保持和控制消息扫描一致且避免 TrimSpace 额外语义。
	i := skipJSONWhitespace(raw, 0)
	if i == len(raw) {
		// 全空白 payload 不能进入 inbox。
		return true
	}
	if !hasLiteralAt(raw, i, "null") {
		// 普通 usage 热路径通常在这里快速返回。
		return false
	}
	// null 后面只允许 JSON 空白，避免误丢 nullx 这类未知内容。
	return skipJSONWhitespace(raw, i+len("null")) == len(raw)
}
