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

// RedisInboxWriter 是远端 ingest 到本地 durable inbox 的唯一写入边界。
type RedisInboxWriter interface {
	// Insert 把 raw usage JSON 批量写入 redis_usage_inboxes；source 用于调用方标记来源。
	Insert(ctx context.Context, source string, messages []string, receivedAt time.Time) (int, error)
}
