package poller

import (
	"time"

	"cpa-usage-keeper/internal/cpa"
)

// RedisIngestSyncMode 表示启动探测后固定下来的长期远端拉取模式。
type RedisIngestSyncMode string

const (
	// RedisIngestSyncModeUnknown 表示尚未完成启动探测或正在重新探测。
	RedisIngestSyncModeUnknown RedisIngestSyncMode = "unknown"
	// RedisIngestSyncModeSubscribe 表示当前优先使用 Redis SUBSCRIBE usage。
	RedisIngestSyncModeSubscribe RedisIngestSyncMode = "subscribe"
	// RedisIngestSyncModeRedisPull 表示启动时订阅不可用，但 Redis batch pull 可用。
	RedisIngestSyncModeRedisPull RedisIngestSyncMode = "redis_pull"
	// RedisIngestSyncModeHTTPPull 表示启动时订阅和 Redis pull 都不可用，只能使用 HTTP pull。
	RedisIngestSyncModeHTTPPull RedisIngestSyncMode = "http_pull"
)

// RedisIngestSubState 表示长期模式内部的临时阶段，用于日志排查状态机动作。
type RedisIngestSubState string

const (
	// RedisIngestSubStateStarting 表示正在做 subscribe -> redis pull -> http pull 启动探测。
	RedisIngestSubStateStarting RedisIngestSubState = "starting"
	// RedisIngestSubStateSubscribeBackfill 表示订阅已连接，正在用旧拉取方式补历史数据。
	RedisIngestSubStateSubscribeBackfill RedisIngestSubState = "subscribe_backfill"
	// RedisIngestSubStateSubscribeReceiving 表示订阅连接稳定，正在等待 Redis 推送 usage 消息。
	RedisIngestSubStateSubscribeReceiving RedisIngestSubState = "subscribe_receiving"
	// RedisIngestSubStateSubscribeDegradedPolling 表示订阅断开后临时降级到 Redis/HTTP 轮询。
	RedisIngestSubStateSubscribeDegradedPolling RedisIngestSubState = "subscribe_degraded_polling"
	// RedisIngestSubStateRedisPullActive 表示固定 redis_pull 模式下 Redis 拉取正常。
	RedisIngestSubStateRedisPullActive RedisIngestSubState = "redis_pull_active"
	// RedisIngestSubStateRedisPullDegradedHTTP 表示固定 redis_pull 模式下 Redis 失败，临时用 HTTP 兜底。
	RedisIngestSubStateRedisPullDegradedHTTP RedisIngestSubState = "redis_pull_degraded_http"
	// RedisIngestSubStateHTTPPullActive 表示固定 http_pull 模式正在运行。
	RedisIngestSubStateHTTPPullActive RedisIngestSubState = "http_pull_active"
)

const (
	// RedisIngestSourceSubscribe 是订阅消息来源标签，只用于状态/测试，不写入 queue_key。
	RedisIngestSourceSubscribe = "redis_subscribe:" + cpa.ManagementUsageSubscribeChannel
	// RedisIngestSourceRedisPull 是旧 Redis queue 拉取来源标签，只用于状态/测试。
	RedisIngestSourceRedisPull = "redis_pull:" + cpa.ManagementUsageQueueKey
	// RedisIngestSourceHTTPPull 是 HTTP usage queue 拉取来源标签，只用于状态/测试。
	RedisIngestSourceHTTPPull = "http_pull:usage_queue"
)

// redisIngestRecoveryRetryInterval 控制 subscribe/Redis 恢复探测间隔。
const redisIngestRecoveryRetryInterval = 30 * time.Second

// redisIngestSubscribeBatchWindow 控制订阅收到首条消息后最多聚合 1s 再写入 inbox。
const redisIngestSubscribeBatchWindow = time.Second
