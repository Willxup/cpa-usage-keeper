package test

import (
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/repository"
)

func TestSumUsageWindowsPrefixSumsAndOrderPreserved(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)

	appendRecentCacheEvents(cache, []entities.UsageEvent{
		{APIGroupKey: "k", Model: "m", Timestamp: now.Add(-30 * time.Second), InputTokens: 100, TotalTokens: 100},
		{APIGroupKey: "k", Model: "m", Timestamp: now.Add(-2 * time.Minute), InputTokens: 200, TotalTokens: 200},
		// 失败请求计入 requests/failures，但不计入 token。
		{APIGroupKey: "k", Model: "m", Timestamp: now.Add(-5 * time.Minute), Failed: true, TotalTokens: 0},
		{APIGroupKey: "k", Model: "m", Timestamp: now.Add(-10 * time.Minute), InputTokens: 300, TotalTokens: 300},
	})

	// 入参顺序故意打乱，验证返回与入参一一对应。
	windows := []time.Duration{15 * time.Minute, time.Minute, 5 * time.Minute}
	rows, err := SumUsageWindows(windows, now, cache, "k", pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver())
	if err != nil {
		t.Fatalf("SumUsageWindows returned error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// 1m：只有 30 秒前那条。
	if rows[1].WindowSeconds != 60 || rows[1].Requests != 1 || rows[1].TotalTokens != 100 {
		t.Fatalf("unexpected 1m row: %+v", rows[1])
	}
	if !rows[1].WindowEnd.Equal(now) || !rows[1].WindowStart.Equal(now.Add(-time.Minute)) {
		t.Fatalf("unexpected 1m window bounds: %+v", rows[1])
	}
	// 5m：加上 2 分钟前那条和 5 分钟前的失败请求。
	if rows[2].Requests != 3 || rows[2].Failures != 1 || rows[2].TotalTokens != 300 {
		t.Fatalf("unexpected 5m row: %+v", rows[2])
	}
	// 15m：再加上 10 分钟前那条。
	if rows[0].Requests != 4 || rows[0].Failures != 1 || rows[0].TotalTokens != 600 {
		t.Fatalf("unexpected 15m row: %+v", rows[0])
	}
}

func TestSumUsageWindowsFiltersAPIGroupAndFailsWhenCacheUnavailable(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	appendRecentCacheEvents(cache, []entities.UsageEvent{
		{APIGroupKey: "k1", Model: "m", Timestamp: now.Add(-10 * time.Second), TotalTokens: 5},
		{APIGroupKey: "k2", Model: "m", Timestamp: now.Add(-10 * time.Second), TotalTokens: 7},
	})

	rows, err := SumUsageWindows([]time.Duration{time.Minute}, now, cache, "k2", pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver())
	if err != nil {
		t.Fatalf("SumUsageWindows returned error: %v", err)
	}
	if rows[0].Requests != 1 || rows[0].TotalTokens != 7 {
		t.Fatalf("api group filter not applied: %+v", rows[0])
	}

	// 缓存不可用时必须返回明确错误，而不是可能被误读成"没流量"的空结果。
	if _, err := SumUsageWindows([]time.Duration{time.Minute}, now, nil, "", pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()); !errors.Is(err, ErrRecentUsageCacheUnavailable) {
		t.Fatalf("expected ErrRecentUsageCacheUnavailable, got %v", err)
	}
	// 超过缓存覆盖范围同样必须失败，不能静默少算。
	if _, err := SumUsageWindows([]time.Duration{71 * time.Minute}, now, cache, "", pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()); !errors.Is(err, ErrRecentUsageCacheUnavailable) {
		t.Fatalf("expected ErrRecentUsageCacheUnavailable for oversized window, got %v", err)
	}
}
