package test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

// TestUsageRecentEventCacheReloadsStoredFeesWithoutResettingHealth 验证费用重载仅替换近期事件，失败后禁用旧费用并保留健康生命周期。
func TestUsageRecentEventCacheReloadsStoredFeesWithoutResettingHealth(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	firstCost, firstAvailable := 1.0, true
	first := entities.UsageEvent{EventKey: "first", APIGroupKey: "key", Model: "model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: base.Add(-20 * time.Minute), CostUSD: &firstCost, CostAvailable: &firstAvailable}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	secondCost, secondAvailable := 1.5, true
	second := entities.UsageEvent{EventKey: "second", APIGroupKey: "key", Model: "model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: base.Add(-10 * time.Minute), CostUSD: &secondCost, CostAvailable: &secondAvailable}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if !cache.TryAppend([]entities.UsageEvent{second}) {
		t.Fatal("accepted append was lost before reload")
	}
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", first.ID).Update("cost_usd", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", second.ID).Update("cost_usd", 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertReloadedRecentCosts(t, cache, base, 2, 3)
	assertReloadHealth(t, cache, base, 2)

	if err := db.Exec("ALTER TABLE usage_events RENAME TO usage_events_hidden").Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err == nil {
		t.Fatal("failed DB reload was reported as success")
	}
	if events, ok := cache.Events(base.Add(-time.Hour), base, false, ""); ok || events != nil {
		t.Fatalf("failed reload exposed stale Events: ok=%t events=%+v", ok, events)
	}
	if events, ok := cache.EventsSince(base.Add(-time.Hour), ""); ok || events != nil {
		t.Fatalf("failed reload exposed stale EventsSince: ok=%t events=%+v", ok, events)
	}
	assertReloadHealth(t, cache, base, 2)
	if err := db.Exec("ALTER TABLE usage_events_hidden RENAME TO usage_events").Error; err != nil {
		t.Fatal(err)
	}
	thirdCost, thirdAvailable := 4.0, true
	third := entities.UsageEvent{EventKey: "third", APIGroupKey: "key", Model: "model", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: base.Add(-time.Minute), CostUSD: &thirdCost, CostAvailable: &thirdAvailable}
	if err := db.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	if !cache.TryAppend([]entities.UsageEvent{third}) {
		t.Fatal("append stopped after failed reload")
	}
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReloadHealth(t, cache, base, 3)
	if events, ok := cache.EventsSince(base.Add(-time.Hour), ""); ok || events != nil {
		t.Fatalf("new append reopened stale fee cache before successful reload: ok=%t events=%+v", ok, events)
	}
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", third.ID).Update("cost_usd", 5).Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err != nil {
		t.Fatalf("retry reload: %v", err)
	}
	assertReloadedRecentCosts(t, cache, base, 2, 3, 5)
	assertReloadHealth(t, cache, base, 3)

	// 时间推进后依旧由正常追加触发 5h 健康桶自然淘汰，而不是靠重载重建。
	clock.Store(base.Add(6 * time.Hour).Unix())
	if !cache.TryAppend([]entities.UsageEvent{{EventKey: "later", APIGroupKey: "key", AuthType: "oauth", AuthIndex: "auth-b", Timestamp: now(), CostUSD: reloadCost(0), CostAvailable: reloadAvailable(false)}}) {
		t.Fatal("append stopped after successful reload")
	}
	if err := cache.DrainAcceptedAppends(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReloadHealth(t, cache, now(), 0)
	health, ok := cache.CredentialHealth("oauth", "auth-b", now())
	if !ok || health.TotalSuccess != 1 {
		t.Fatalf("new health bucket after reload: %+v ok=%t", health, ok)
	}
}

// TestUsageRecentEventCacheReloadCancellationInvalidatesOnlyEventReads 验证取消不误报空成功，后续成功重载可恢复。
func TestUsageRecentEventCacheReloadCancellationInvalidatesOnlyEventReads(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	event := entities.UsageEvent{EventKey: "old", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-time.Minute), CostUSD: reloadCost(1), CostAvailable: reloadAvailable(true)}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.ReloadStoredCostEvents(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reload: %v", err)
	}
	if _, ok := cache.Events(now.Add(-time.Hour), now, false, ""); ok {
		t.Fatal("canceled reload left stale Events enabled")
	}
	assertReloadHealth(t, cache, now, 1)
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertReloadedRecentCosts(t, cache, now, 1)
}

// TestUsageRecentEventCacheReloadRejectsIncompleteFees 验证未回填费用不能被当成空缓存或明确零费用。
func TestUsageRecentEventCacheReloadRejectsIncompleteFees(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	event := entities.UsageEvent{EventKey: "unbackfilled", AuthType: "oauth", AuthIndex: "auth-a", Timestamp: now.Add(-time.Minute), CostUSD: reloadCost(1), CostAvailable: reloadAvailable(true)}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", event.ID).Updates(map[string]any{"cost_usd": nil, "cost_available": nil}).Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err == nil {
		t.Fatal("NULL stored fee was accepted as an empty or zero-fee reload")
	}
	if events, ok := cache.EventsSince(now.Add(-time.Hour), ""); ok || events != nil {
		t.Fatalf("incomplete fee left stale cache visible: %+v ok=%t", events, ok)
	}
	assertReloadHealth(t, cache, now, 1)
	if err := db.Model(&entities.UsageEvent{}).Where("id = ?", event.ID).Updates(map[string]any{"cost_usd": 0.0, "cost_available": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	events, ok := cache.EventsSince(now.Add(-time.Hour), "")
	if !ok || len(events) != 1 || events[0].CostUSD == nil || *events[0].CostUSD != 0 || events[0].CostAvailable == nil || *events[0].CostAvailable {
		t.Fatalf("explicit zero/unavailable fee was not restored: %+v ok=%t", events, ok)
	}
	if err := db.Delete(&entities.UsageEvent{}, event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := cache.ReloadStoredCostEvents(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if events, ok := cache.EventsSince(now.Add(-time.Hour), ""); !ok || len(events) != 0 {
		t.Fatalf("successful empty reload retained old events: %+v ok=%t", events, ok)
	}
	assertReloadHealth(t, cache, now, 1)
}

func assertReloadedRecentCosts(t *testing.T, cache *repository.UsageRecentEventCache, now time.Time, want ...float64) {
	t.Helper()
	events, ok := cache.EventsSince(now.Add(-time.Hour), "")
	if !ok || len(events) != len(want) {
		t.Fatalf("reloaded costs count=%d ok=%t, want %d", len(events), ok, len(want))
	}
	for index, cost := range want {
		if events[index].CostUSD == nil || *events[index].CostUSD != cost || events[index].CostAvailable == nil || !*events[index].CostAvailable {
			t.Fatalf("reloaded event %d fee=%+v, want %g/true", index, events[index], cost)
		}
	}
}

func assertReloadHealth(t *testing.T, cache *repository.UsageRecentEventCache, now time.Time, want int64) {
	t.Helper()
	health, ok := cache.CredentialHealth("oauth", "auth-a", now)
	if !ok || health.TotalSuccess != want {
		t.Fatalf("health after fee reload: %+v ok=%t, want success=%d", health, ok, want)
	}
}

func reloadCost(value float64) *float64 { return &value }
func reloadAvailable(value bool) *bool  { return &value }
