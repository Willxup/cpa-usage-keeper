package poller

import (
	"context"
	"time"
)

// UsageSubscriptionSource 是订阅连接工厂，runner 只依赖接口，不关心 TCP/RESP 细节。
type UsageSubscriptionSource interface {
	// Subscribe 建立 Redis SUBSCRIBE usage 连接；只有验证订阅 ack 后才返回成功。
	Subscribe(ctx context.Context) (UsageSubscription, error)
}

// UsageSubscription 表示一个已经成功订阅 usage channel 的长期连接。
type UsageSubscription interface {
	// Receive 阻塞读取下一条 raw usage JSON；ctx 控制 batch window 或应用关停。
	Receive(ctx context.Context) (string, error)
	// Close 关闭订阅连接，断线降级和应用关停都会调用。
	Close() error
}

// UsagePullSource 表示一次性批量拉取 raw usage JSON 的来源。
type UsagePullSource interface {
	// Pull 只负责拉取，不负责 fallback、不负责落库。
	Pull(ctx context.Context) ([]string, error)
}

// UsagePullSourceNamer 可在拉取成功后提供更精确的来源名，例如 redis_pull 选定的 usage/queue key。
type UsagePullSourceNamer interface {
	SourceName() string
}

// RedisInboxWriter 是远端 ingest 到本地 durable inbox 的唯一写入边界。
type RedisInboxWriter interface {
	// Insert 把 raw usage JSON 批量写入 redis_usage_inboxes；source 会原样落库。
	Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error)
}

// RedisControlMessageObserver 接收 usage ingest 的连接和 metadata 控制信号。
type RedisControlMessageObserver interface {
	// NotifyIngestConnected 表示 usage ingest 已成功建立或恢复一条 CPA 连接。
	NotifyIngestConnected()
	// MarkRefreshSupported 表示 CPA 已支持 metadata refresh 通知，周期轮询可以 no-op。
	MarkRefreshSupported()
	// RequestMetadataRefresh 表示 CPA metadata 配置已变化，需要 debounce 后同步。
	RequestMetadataRefresh()
	// MarkRefreshPollingRequired 表示 usage 链路降级或失败，metadata 同步必须恢复轮询。
	MarkRefreshPollingRequired(reason string)
}
