package poller

import (
	"context"

	"cpa-usage-keeper/internal/cpa"
)

type RedisPullSource struct {
	// client 只保留 Redis queue client；HTTP fallback 已经移到 runner 状态机。
	client *cpa.RedisQueueClient
}

func NewRedisPullSource(opts cpa.RedisQueueOptions) *RedisPullSource {
	// Redis source 只构造 client，不主动连接 Redis。
	return &RedisPullSource{client: cpa.NewRedisQueueClientWithOptions(opts)}
}

func (s *RedisPullSource) Pull(ctx context.Context) ([]string, error) {
	if s == nil || s.client == nil {
		// Redis client 缺失时返回 Redis 相关错误，让 runner 走 fallback。
		return nil, cpa.ErrRedisQueueAuth
	}
	// PopUsage 现在是 Redis-only；是否降级 HTTP 完全由 RedisIngestRunner 决定。
	return s.client.PopUsage(ctx)
}
