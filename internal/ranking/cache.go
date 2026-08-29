package ranking

import (
	"sync"
	"time"
)

const leaderboardReadCacheTTL = 30 * time.Second

type leaderboardCacheKey struct {
	period LeaderboardPeriod
	metric LeaderboardMetric
}

type leaderboardCacheEntry struct {
	value     Leaderboard
	expiresAt time.Time
}

type metadataCacheEntry struct {
	value     LeaderboardMetadata
	expiresAt time.Time
}

// readCache 只保存中心成功响应；固定周期和指标集合使榜单 map 最多包含 32 项。
type readCache struct {
	mu           sync.Mutex
	leaderboards map[leaderboardCacheKey]leaderboardCacheEntry
	metadata     *metadataCacheEntry
}

func newReadCache() *readCache {
	return &readCache{leaderboards: make(map[leaderboardCacheKey]leaderboardCacheEntry, 32)}
}

func (c *readCache) leaderboard(period LeaderboardPeriod, metric LeaderboardMetric, now time.Time) (Leaderboard, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := leaderboardCacheKey{period: period, metric: metric}
	entry, ok := c.leaderboards[key]
	if !ok || !now.Before(entry.expiresAt) {
		delete(c.leaderboards, key)
		return Leaderboard{}, false
	}
	return entry.value, true
}

func (c *readCache) storeLeaderboard(period LeaderboardPeriod, metric LeaderboardMetric, value Leaderboard, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leaderboards[leaderboardCacheKey{period: period, metric: metric}] = leaderboardCacheEntry{
		value: value, expiresAt: now.Add(leaderboardReadCacheTTL),
	}
}

func (c *readCache) leaderboardMetadata(now time.Time) (LeaderboardMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metadata == nil || !now.Before(c.metadata.expiresAt) {
		c.metadata = nil
		return LeaderboardMetadata{}, false
	}
	return c.metadata.value, true
}

func (c *readCache) storeLeaderboardMetadata(value LeaderboardMetadata, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata = &metadataCacheEntry{value: value, expiresAt: now.Add(leaderboardReadCacheTTL)}
}
