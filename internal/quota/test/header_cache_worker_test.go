package test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"

	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"gorm.io/gorm"
)

func TestApplyUsageHeaderSnapshotWritesCompletedCacheWithWindowUsageStats(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:     "oauth",
		AuthIndex:    "codex-auth",
		Model:        "gpt-5.5",
		Timestamp:    time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		TotalTokens:  123,
		InputTokens:  100,
		OutputTokens: 23,
	})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshotWithHeaders(
		"codex-auth",
		time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		http.Header{
			"X-Codex-Plan-Type":              []string{"pro"},
			"X-Codex-Primary-Used-Percent":   []string{"4"},
			"X-Codex-Primary-Window-Minutes": []string{"300"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local).Unix(), 10)},
		},
	))
	if !applied {
		t.Fatal("expected header snapshot to apply")
	}
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Status != RefreshTaskStatusCompleted || task.Quota == nil || len(task.Quota.Quota) != 1 {
		t.Fatalf("unexpected task: %+v", task)
	}
	if task.Quota.Subscription == nil || task.Quota.Subscription.Provider != "codex" || task.Quota.Subscription.Plan != "pro-20x" {
		t.Fatalf("expected header subscription, got %+v", task.Quota.Subscription)
	}
	row := task.Quota.Quota[0]
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 123 || row.WindowUsageCost == nil {
		t.Fatalf("expected local token/cost fallback, got %#v", row)
	}
	if task.RefreshedAt == nil || !task.RefreshedAt.Equal(time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)) {
		t.Fatalf("expected refreshed_at from observed_at, got %+v", task.RefreshedAt)
	}
}

func TestApplyUsageHeaderSnapshotStoresUsageIdentityDisplayName(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Name: "   ", Provider: "Codex Team", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshotWithHeaders(
		"codex-auth",
		time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		http.Header{
			"X-Codex-Plan-Type":              []string{"pro"},
			"X-Codex-Primary-Used-Percent":   []string{"4"},
			"X-Codex-Primary-Window-Minutes": []string{"300"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local).Unix(), 10)},
		},
	))
	if !applied {
		t.Fatal("expected header snapshot to apply")
	}

	task := refreshTaskRecord(service, "codex-auth")
	if task == nil || task.Name != "Codex Team" {
		t.Fatalf("expected header cache to store display name, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotUsesObservedAtAsWindowUsageStatsEnd(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	observedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:    "oauth",
		AuthIndex:   "codex-auth",
		Model:       "gpt-5.5",
		Timestamp:   observedAt.Add(-time.Nanosecond),
		TotalTokens: 50,
	})
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:    "oauth",
		AuthIndex:   "codex-auth",
		Model:       "gpt-5.5",
		Timestamp:   observedAt,
		TotalTokens: 123,
	})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshotWithHeaders(
		"codex-auth",
		observedAt,
		http.Header{
			"X-Codex-Plan-Type":              []string{"pro"},
			"X-Codex-Primary-Used-Percent":   []string{"4"},
			"X-Codex-Primary-Window-Minutes": []string{"300"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(observedAt.Add(4*time.Hour).Unix(), 10)},
		},
	))
	if !applied {
		t.Fatal("expected header snapshot to apply")
	}
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 {
		t.Fatalf("unexpected task quota: %+v", task)
	}
	row := task.Quota.Quota[0]
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 50 {
		t.Fatalf("expected local token fallback to use observed_at as half-open window end, got %#v", row)
	}
}

func TestApplyUsageHeaderSnapshotMatchesUsageIdentityTypeByAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "Codex Team", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	snapshot := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	snapshot.Provider = "claude"
	applied := applyUsageHeaderSnapshot(service, context.Background(), snapshot)
	if !applied {
		t.Fatal("expected auth_index usage identity type to drive codex header matching")
	}
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected codex quota cache from identity type, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotIgnoresProviderOnlyCodexWhenIdentityTypeDiffers(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "codex", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("claude-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if applied {
		t.Fatal("expected non-codex usage identity type to ignore codex-looking headers")
	}
	if refreshTaskCount(service) != 0 {
		t.Fatalf("expected no quota cache task, got %+v", refreshTasks(service))
	}
}

func TestApplyUsageHeaderSnapshotSkipsActiveRefreshTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{AuthIndex: "codex-auth", Status: RefreshTaskStatusQueued, Source: RefreshSourceManual}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if applied {
		t.Fatal("expected active refresh task to win over header snapshot")
	}
	if task := refreshTasks(service)["codex-auth"]; task.Status != RefreshTaskStatusQueued || task.Quota != nil {
		t.Fatalf("expected queued task to remain unchanged, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotUpdatesRecentCompletedCacheAndCreatesMissingCache(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "new-codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: refreshedAt,
		Quota:       &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", Scope: "window", UsedPercent: floatPtr(90)}}},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected recent newer header to update completed cache")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected recent header progress to update cache, got %+v", task)
	}

	applied = applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("new-codex-auth", refreshedAt.Add(20*time.Second), "8"))
	if !applied {
		t.Fatal("expected missing cache to be created despite debounce window")
	}
	created := refreshTasks(service)["new-codex-auth"]
	if created == nil || created.Quota == nil || len(created.Quota.Quota) != 1 || created.Quota.Quota[0].UsedPercent == nil || *created.Quota.Quota[0].UsedPercent != 8 {
		t.Fatalf("expected header quota cache creation for missing cache, got %+v", created)
	}
}

func TestApplyUsageHeaderSnapshotUpdatesRecentCompletedCacheAndRefreshesWindowUsageStats(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:    "oauth",
		AuthIndex:   "codex-auth",
		Model:       "gpt-5.5",
		Timestamp:   time.Date(2026, 6, 22, 10, 30, 0, 0, time.Local),
		TotalTokens: 123,
	})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: refreshedAt,
		Quota: &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{
			Key:         "rate_limit.primary_window",
			Label:       "5h",
			Scope:       "window",
			UsedPercent: floatPtr(90),
		}}},
	}
	windowStatsQueries := 0
	priceQueries := 0
	callbackName := "test:count_header_recent_update_window_stats_queries"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if queryMentionsTable(sql, "usage_events") || queryMentionsTable(sql, "usage_overview_hourly_stats") {
			windowStatsQueries++
		}
		if queryMentionsTable(sql, "model_price_settings") {
			priceQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected recent newer header to update completed cache")
	}
	if priceQueries != 0 {
		t.Fatalf("expected cached pricing to avoid model price queries, got %d", priceQueries)
	}
	_ = windowStatsQueries
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected recent header progress to update cache, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotsDoesNotDependOnPriceTableQueries(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	callbackName := "test:fail_header_batch_window_stats_provider"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if queryMentionsTable(tx.Statement.SQL.String(), "model_price_settings") {
			tx.AddError(fmt.Errorf("forced model price settings failure"))
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applyUsageHeaderSnapshots(service, context.Background(), []UsageHeaderSnapshot{
		codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"),
	})

	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); err != nil {
		t.Fatalf("expected cached pricing to avoid injected price-query failure, got err=%v", err)
	}
}

func TestApplyUsageHeaderSnapshotsAllowsNilService(t *testing.T) {
	// nil service 模拟防御性调用路径，确保批量 apply 和其它 Service 方法一样安全 no-op。
	var service *Service
	// recover 捕获 panic，把 nil receiver 崩溃转成明确的测试失败。
	defer func() {
		// 非 nil recovered 说明当前实现仍然解引用了 nil service。
		if recovered := recover(); recovered != nil {
			t.Fatalf("expected nil service batch apply to no-op, got panic: %v", recovered)
		}
	}()

	// 非空 snapshot 批次会越过 len(snapshots)==0 分支，真实覆盖 review 指出的 nil receiver 路径。
	applyUsageHeaderSnapshots(service, context.Background(), []UsageHeaderSnapshot{
		codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"),
	})
}

func TestApplyUsageHeaderSnapshotWarnsOnIdentityDatabaseError(t *testing.T) {
	hook := logrustest.NewGlobal()
	defer hook.Reset()
	previousLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() { logrus.SetLevel(previousLevel) })

	service := NewServiceWithRegistry(nil, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	if applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Now(), "4")) {
		t.Fatal("expected snapshot with database error to be ignored")
	}
	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel && entry.Message == "usage header quota identity lookup failed" && entry.Data["auth_index"] == "codex-auth" {
			return
		}
	}
	t.Fatalf("expected warning log for identity database error, got %#v", hook.AllEntries())
}

func TestApplyUsageHeaderSnapshotRecoversFailedCacheWithinDebounceAndClearsFailureFields(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	httpStatus := 429
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:      "codex-auth",
		Status:         RefreshTaskStatusFailed,
		Error:          "rate limited",
		HTTPStatusCode: &httpStatus,
		Source:         RefreshSourceManual,
		RefreshedAt:    refreshedAt,
		ExpiresAt:      time.Now().Add(time.Hour),
		Quota:          &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", Scope: "window", UsedPercent: floatPtr(90)}}},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected failed cache to be recovered by complete header inside debounce window")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Status != RefreshTaskStatusCompleted || task.Error != "" || task.HTTPStatusCode != nil || !task.ExpiresAt.IsZero() {
		t.Fatalf("expected failed fields to be cleared after header recovery, got %+v", task)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected recovered header quota, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotDoesNotOverwriteNewerCompletedCache(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	newerAt := time.Date(2026, 6, 22, 12, 0, 0, 0, time.Local)
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: newerAt,
		Quota:       &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", UsedPercent: floatPtr(90)}}},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", newerAt.Add(-time.Hour), "4"))
	if applied {
		t.Fatal("expected older header snapshot to be ignored")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 90 {
		t.Fatalf("expected newer cache to remain unchanged, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotIgnoresIncompleteWindowWithoutClearingExistingUsage(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	oldPercent := 80.0
	oldTokens := int64(999)
	oldCost := 9.9
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		Quota: &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{
			Key:               "rate_limit.primary_window",
			Label:             "5h",
			Scope:             "window",
			UsedPercent:       &oldPercent,
			Window:            &QuotaWindow{Seconds: intPtr(quotaWindowFiveHourSeconds)},
			ResetAfterSeconds: intPtr(3600),
			WindowUsageTokens: &oldTokens,
			WindowUsageCost:   &oldCost,
		}}},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
	})
	if applied {
		t.Fatal("expected incomplete header window to be ignored")
	}
	task := refreshTasks(service)["codex-auth"]
	row := task.Quota.Quota[0]
	if row.UsedPercent == nil || *row.UsedPercent != oldPercent || row.WindowUsageTokens == nil || *row.WindowUsageTokens != oldTokens || row.WindowUsageCost == nil || *row.WindowUsageCost != oldCost {
		t.Fatalf("expected existing cache usage fields to remain unchanged, got %#v", row)
	}
}

func TestApplyUsageHeaderSnapshotMergesProgressWithManualAuthoritativeFields(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:    "oauth",
		AuthIndex:   "codex-auth",
		Model:       "gpt-5.5",
		Timestamp:   time.Date(2026, 6, 22, 10, 30, 0, 0, time.Local),
		TotalTokens: 123,
	})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	oldUsed := 8.0
	oldLimit := 10.0
	oldRemaining := 2.0
	oldRemainingFraction := 0.2
	oldPercent := 80.0
	oldTokens := int64(999)
	oldCost := 9.9
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		Quota: &CheckResponse{ID: "codex-auth", Subscription: &SubscriptionInfo{Provider: "codex", Plan: "plus"}, Quota: []QuotaRow{{
			Key:               "rate_limit.primary_window",
			Label:             "Manual 5h",
			Scope:             "window",
			Metric:            "manual",
			Used:              &oldUsed,
			Limit:             &oldLimit,
			Remaining:         &oldRemaining,
			RemainingFraction: &oldRemainingFraction,
			UsedPercent:       &oldPercent,
			Window:            &QuotaWindow{Seconds: intPtr(quotaWindowFiveHourSeconds)},
			WindowUsageTokens: &oldTokens,
			WindowUsageCost:   &oldCost,
		}}},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if !applied {
		t.Fatal("expected header snapshot to update stale cache")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 {
		t.Fatalf("unexpected merged task: %+v", task)
	}
	row := task.Quota.Quota[0]
	if task.Quota.Subscription == nil || task.Quota.Subscription.Plan != "plus" {
		t.Fatalf("expected header without plan to preserve cached subscription, got %+v", task.Quota.Subscription)
	}
	if row.Used == nil || *row.Used != oldUsed || row.Limit == nil || *row.Limit != oldLimit || row.Remaining == nil || *row.Remaining != oldRemaining || row.RemainingFraction == nil || *row.RemainingFraction != oldRemainingFraction {
		t.Fatalf("expected manual absolute fields to be preserved, got %#v", row)
	}
	if row.UsedPercent == nil || *row.UsedPercent != 4 {
		t.Fatalf("expected header used percent to update progress, got %#v", row.UsedPercent)
	}
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 123 || row.WindowUsageCost == nil {
		t.Fatalf("expected window token/cost fallback to follow header progress, got %#v", row)
	}
}

func TestApplyUsageHeaderSnapshotOverridesCachedSubscriptionWhenHeaderHasPlan(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		Quota: &CheckResponse{
			ID:           "codex-auth",
			Subscription: &SubscriptionInfo{Provider: "codex", Plan: "plus"},
			Quota:        []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", UsedPercent: floatPtr(80)}},
		},
	}

	header := codexUsageHeader("4")
	header.Set("X-Codex-Plan-Type", "pro")
	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshotWithHeaders(
		"codex-auth",
		time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		header,
	))
	if !applied {
		t.Fatal("expected newer header snapshot to apply")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || task.Quota.Subscription == nil || task.Quota.Subscription.Plan != "pro-20x" {
		t.Fatalf("expected header subscription to override cache, got %+v", task.Quota)
	}
}

func TestApplyUsageHeaderSnapshotMergesRowsAndPreservesResetCredits(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	credits := 2
	oldPercent := 61.0
	refreshTasks(service)["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		Quota: &CheckResponse{
			ID:                                  "codex-auth",
			RateLimitResetCreditsAvailableCount: &credits,
			Quota: []QuotaRow{
				{Key: "rate_limit.primary_window", Label: "5h", UsedPercent: &oldPercent},
				{Key: "rate_limit.secondary_window", Label: "Weekly", UsedPercent: floatPtr(22)},
				{Key: "manual_only", Label: "Manual Only", Scope: "extra"},
			},
		},
	}

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if !applied {
		t.Fatal("expected newer header snapshot to apply")
	}
	task := refreshTasks(service)["codex-auth"]
	if task.Quota == nil || task.Quota.RateLimitResetCreditsAvailableCount == nil || *task.Quota.RateLimitResetCreditsAvailableCount != 2 {
		t.Fatalf("expected reset credits to be preserved, got %+v", task.Quota)
	}
	if len(task.Quota.Quota) != 3 {
		t.Fatalf("expected merged rows to preserve non-header rows, got %#v", task.Quota.Quota)
	}
	if task.Quota.Quota[0].Key != "rate_limit.primary_window" || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected primary row to be replaced by header row, got %#v", task.Quota.Quota[0])
	}
	if task.Quota.Quota[1].Key != "rate_limit.secondary_window" || task.Quota.Quota[2].Key != "manual_only" {
		t.Fatalf("expected untouched rows to keep their order, got %#v", task.Quota.Quota)
	}
}

func TestApplyUsageHeaderSnapshotDoesNotBackfillAdditionalLimitUsageStats(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageEvent(t, db, entities.UsageEvent{
		AuthType:    "oauth",
		AuthIndex:   "codex-auth",
		Model:       "gpt-5.5",
		Timestamp:   time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		TotalTokens: 123,
	})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	applied := applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshotWithHeaders(
		"codex-auth",
		time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		http.Header{
			"X-Codex-Bengalfox-Limit-Name":                  []string{"GPT-5.3-Codex-Spark"},
			"X-Codex-Bengalfox-Primary-Used-Percent":        []string{"5"},
			"X-Codex-Bengalfox-Primary-Window-Minutes":      []string{"300"},
			"X-Codex-Bengalfox-Primary-Reset-After-Seconds": []string{"60"},
		},
	))
	if !applied {
		t.Fatal("expected additional header snapshot to apply")
	}
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 {
		t.Fatalf("unexpected task quota: %+v", task)
	}
	row := task.Quota.Quota[0]
	if row.Scope != "additional" || row.WindowUsageTokens != nil || row.WindowUsageCost != nil {
		t.Fatalf("expected additional limit to skip auth-wide usage fallback, got %#v", row)
	}
}

func TestApplyUsageHeaderSnapshotIgnoresUnsupportedSnapshots(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "provider-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAIProvider})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "deleted-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, IsDeleted: true})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": nil}), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	// 从一份合法不可变快照复制测试输入，只改变身份边界；最后一项保留空 cache 投影。
	valid := codexUsageHeaderSnapshot("codex-auth", time.Now(), "4")
	invalidAuthType := valid
	invalidAuthType.AuthType = "apikey"
	missingAuthIndex := valid
	missingAuthIndex.AuthIndex = ""
	providerIdentity := valid
	providerIdentity.AuthIndex = "provider-auth"
	deletedIdentity := valid
	deletedIdentity.AuthIndex = "deleted-auth"
	claudeIdentity := valid
	claudeIdentity.AuthIndex = "claude-auth"
	tests := []UsageHeaderSnapshot{
		invalidAuthType,
		missingAuthIndex,
		providerIdentity,
		deletedIdentity,
		claudeIdentity,
		{AuthType: "oauth", AuthIndex: "codex-auth", Provider: "codex", ObservedAt: time.Now()},
	}
	for _, snapshot := range tests {
		if applyUsageHeaderSnapshot(service, context.Background(), snapshot) {
			t.Fatalf("expected snapshot to be ignored: %+v", snapshot)
		}
	}
	if refreshTaskCount(service) != 0 {
		t.Fatalf("expected no header cache tasks, got %+v", refreshTasks(service))
	}
}

func TestStopRefreshTasksStopsUsageHeaderWorker(t *testing.T) {
	service := NewServiceWithRegistry(openQuotaTestDatabase(t), NewProviderRegistry(nil), emptyPricingCatalogForTest())
	service.StopRefreshTasks()

	if service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("codex-auth", time.Now(), "4"))) {
		t.Fatal("expected stopped usage header worker to reject new snapshots")
	}
}

func TestNewServiceUsesOneMinuteUsageHeaderSnapshotFlushInterval(t *testing.T) {
	// 默认构造 service，用生产默认值初始化 usage header snapshot worker。
	service := NewServiceWithRegistry(openQuotaTestDatabase(t), NewProviderRegistry(nil), emptyPricingCatalogForTest())
	// 测试结束时关闭 worker，避免后台 goroutine 泄漏到后续测试。
	defer service.StopRefreshTasks()

	// 默认 flush 间隔必须保持 1 分钟，防止高频 header 写 cache 又退回 30s。
	if usageHeaderFlushInterval(service) != time.Minute {
		t.Fatalf("expected default usage header snapshot flush interval 1m, got %s", usageHeaderFlushInterval(service))
	}
}

func TestTryAppendUsageHeaderSnapshotsWaitsForCacheFlushBeforeApplying(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	// 修改构造时的原始 Header 不能影响已经结构化并发布的不可变快照。
	originalHeaders := codexUsageHeader("4")
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType: "oauth", AuthIndex: "codex-auth", Provider: "codex",
		ObservedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), Headers: originalHeaders,
	})
	if !ok {
		t.Fatal("expected immutable snapshot to build")
	}
	if !service.TryAppendUsageHeaderSnapshots([]*UsageHeaderSnapshot{snapshot}) {
		t.Fatal("expected snapshot append to be accepted")
	}
	originalHeaders.Set("X-Codex-Primary-Used-Percent", "99")

	time.Sleep(30 * time.Millisecond)
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); err == nil {
		t.Fatal("expected snapshot to remain pending before flush interval")
	}

	service.StopRefreshTasks()
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected stopped worker to flush the published immutable snapshot, got %+v", task)
	}
}

func TestTryAppendUsageHeaderSnapshotsFlushesPendingSnapshotsOnInterval(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: 20 * time.Millisecond, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()

	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))) {
		t.Fatal("expected snapshot append to be accepted")
	}

	task := waitForRefreshTask(t, service, "codex-auth", RefreshTaskStatusCompleted)
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected interval flush to apply header snapshot, got %+v", task)
	}
}

func TestTryAppendUsageHeaderSnapshotsStartsOneShotWindowFromFirstSnapshot(t *testing.T) {
	// 用手动 timer 验证首条 Header 才创建窗口，避免墙钟和 CI 调度暂停制造假失败。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	timerStarted := make(chan time.Duration, 1)
	timerFired := make(chan time.Time, 1)
	setUsageHeaderTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		timerStarted <- delay
		return timerFired, func() {}
	})

	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("codex-auth", time.Now(), "4"))) {
		t.Fatal("expected snapshot append to be accepted")
	}
	select {
	case delay := <-timerStarted:
		if delay != time.Hour {
			t.Fatalf("expected full one-shot interval 1h, got %s", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("expected first snapshot to create the one-shot timer")
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); err == nil {
		t.Fatal("expected snapshot to remain pending until the timer is manually fired")
	}

	timerFired <- time.Now()
	task := waitForRefreshTask(t, service, "codex-auth", RefreshTaskStatusCompleted)
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected one-shot window to flush header snapshot, got %+v", task)
	}
}

func TestUsageHeaderWorkerStaysSilentWithoutSnapshotsAndDoesNotResetActiveWindow(t *testing.T) {
	// 手动 timer 同时锁定“空闲不创建”和“后续 Header 不重置”两个生命周期边界。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	timers := make(chan usageHeaderManualTimer, 2)
	setUsageHeaderTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	// 空闲观察期内 worker 只能阻塞等待，不能创建 timer、查询数据库或写入 quota cache。
	var databaseQueries atomic.Int64
	queryCallback := "test:count_silent_header_queries"
	rowCallback := "test:count_silent_header_rows"
	if err := db.Callback().Query().After("gorm:query").Register(queryCallback, func(*gorm.DB) { databaseQueries.Add(1) }); err != nil {
		t.Fatalf("register silent header query callback: %v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(rowCallback, func(*gorm.DB) { databaseQueries.Add(1) }); err != nil {
		t.Fatalf("register silent header row callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Row().Remove(rowCallback)
	})
	select {
	case timer := <-timers:
		t.Fatalf("expected no timer without Header, got delay=%s", timer.delay)
	case <-time.After(30 * time.Millisecond):
	}
	if got := databaseQueries.Load(); got != 0 {
		t.Fatalf("expected no database work without Header, got %d queries", got)
	}
	if refreshTaskCount(service) != 0 {
		t.Fatalf("expected no cache without Header, got %+v", refreshTasks(service))
	}

	// 首条 Header 创建唯一窗口，第二条只覆盖 pending，不允许创建或重置另一只 timer。
	first := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	second := codexUsageHeaderSnapshot("codex-auth", first.ObservedAt.Add(10*time.Second), "9")
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first)) {
		t.Fatal("expected first Header to be accepted")
	}
	var activeTimer usageHeaderManualTimer
	select {
	case activeTimer = <-timers:
		if activeTimer.delay != time.Hour {
			t.Fatalf("expected one-hour test window, got %s", activeTimer.delay)
		}
	case <-time.After(time.Second):
		t.Fatal("expected first Header to create a timer")
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(second)) {
		t.Fatal("expected second Header to be accepted")
	}
	select {
	case timer := <-timers:
		t.Fatalf("expected second Header not to reset timer, got delay=%s", timer.delay)
	case <-time.After(30 * time.Millisecond):
	}
	// 独立 history runner 会等待自己的十秒批次窗口；一分钟 cache 仍不得在自己的 timer 前应用结果。
	if refreshTaskCount(service) != 0 {
		t.Fatalf("expected pending cache window to remain unapplied, got %+v", refreshTasks(service))
	}

	// 原 timer 到期后一次 flush 使用同身份较新的快照，并在无新数据时重新静默。
	activeTimer.fire <- time.Now()
	task := waitForRefreshTask(t, service, "codex-auth", RefreshTaskStatusCompleted)
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 9 {
		t.Fatalf("expected latest pending Header after one window, got %+v", task)
	}
	select {
	case timer := <-timers:
		t.Fatalf("expected no follow-up timer without new Header, got delay=%s", timer.delay)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestUsageHeaderArrivingDuringFlushStartsNextWindow(t *testing.T) {
	// 第一批身份查询被人为暂停，用真实 worker 证明 flush 期间的新 Header 不会混入当前批次。
	db := openQuotaTestDatabase(t)
	for _, authIndex := range []string{"flush-auth-1", "flush-auth-2"} {
		seedUsageIdentity(t, db, entities.UsageIdentity{Identity: authIndex, Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	timers := make(chan usageHeaderManualTimer, 2)
	setUsageHeaderTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	flushEntered := make(chan struct{}, 1)
	releaseFlush := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFlush) }) }
	defer release()
	var blocked atomic.Bool
	callbackName := "test:block_first_header_flush"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" && blocked.CompareAndSwap(false, true) {
			flushEntered <- struct{}{}
			<-releaseFlush
		}
	}); err != nil {
		t.Fatalf("register first flush blocker: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("flush-auth-1", time.Now(), "4"))) {
		t.Fatal("expected first flush Header to be accepted")
	}
	firstTimer := <-timers
	firstTimer.fire <- time.Now()
	select {
	case <-flushEntered:
	case <-time.After(time.Second):
		t.Fatal("expected first Header flush to enter identity query")
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("flush-auth-2", time.Now().Add(time.Second), "8"))) {
		t.Fatal("expected Header arriving during flush to be accepted")
	}
	release()
	waitForRefreshTask(t, service, "flush-auth-1", RefreshTaskStatusCompleted)

	// 第一批返回后 worker 才消费第二条 Header，并为它创建完整的新窗口。
	var secondTimer usageHeaderManualTimer
	select {
	case secondTimer = <-timers:
		if secondTimer.delay != time.Hour {
			t.Fatalf("expected full next window, got %s", secondTimer.delay)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Header received during flush to create the next timer")
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "flush-auth-2"); err == nil {
		t.Fatal("expected second Header to remain pending before its own timer fires")
	}
	secondTimer.fire <- time.Now()
	secondTask := waitForRefreshTask(t, service, "flush-auth-2", RefreshTaskStatusCompleted)
	if secondTask.Quota == nil || len(secondTask.Quota.Quota) != 1 || secondTask.Quota.Quota[0].UsedPercent == nil || *secondTask.Quota.Quota[0].UsedPercent != 8 {
		t.Fatalf("expected second-window Header cache, got %+v", secondTask)
	}
}

func TestUsageHeaderSlowFlushKeepsLatestIdentityWithoutBatchQueueOverflow(t *testing.T) {
	// 同一身份在慢 flush 期间连续更新只应占一个 pending 槽位，不能因为累计 100 个批次而拒绝最后值。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "latest-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	timers := make(chan usageHeaderManualTimer, 2)
	setUsageHeaderTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	flushEntered := make(chan struct{}, 1)
	releaseFlush := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFlush) }) }
	defer release()
	var blocked atomic.Bool
	callbackName := "test:block_header_flush_while_latest_updates_arrive"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" && blocked.CompareAndSwap(false, true) {
			flushEntered <- struct{}{}
			<-releaseFlush
		}
	}); err != nil {
		t.Fatalf("register slow Header flush blocker: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	baseTime := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(codexUsageHeaderSnapshot("latest-auth", baseTime, "4"))) {
		t.Fatal("expected initial Header to be accepted")
	}
	firstTimer := <-timers
	firstTimer.fire <- time.Now()
	select {
	case <-flushEntered:
	case <-time.After(time.Second):
		t.Fatal("expected initial Header flush to enter identity query")
	}

	// 101 次独立投递会填满旧的 100-batch channel；新结构应始终只保存 latest-auth 的最新值。
	for index := 1; index <= 101; index++ {
		usedPercent := "8"
		if index == 101 {
			usedPercent = "9"
		}
		snapshot := codexUsageHeaderSnapshot("latest-auth", baseTime.Add(time.Duration(index)*time.Second), usedPercent)
		if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshot)) {
			t.Fatalf("expected repeated Header update %d to be accepted", index)
		}
	}
	release()
	waitForRefreshTask(t, service, "latest-auth", RefreshTaskStatusCompleted)

	var secondTimer usageHeaderManualTimer
	select {
	case secondTimer = <-timers:
	case <-time.After(time.Second):
		t.Fatal("expected repeated updates to create one next-window timer")
	}
	secondTimer.fire <- time.Now()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "latest-auth")
		if err == nil && task.Quota != nil && len(task.Quota.Quota) == 1 && task.Quota.Quota[0].UsedPercent != nil && *task.Quota.Quota[0].UsedPercent == 9 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("latest Header value did not replace the first flush result")
}

func TestApplyUsageHeaderSnapshotsProcessesAtMostTwoIdentitiesConcurrently(t *testing.T) {
	// 三个账号共用一批 identity/resolver/calculator，但身份 job 必须有两个并发槽位。
	db, closePools := openQuotaReaderPoolTestDatabase(t)
	defer closePools()
	now := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	for index := 1; index <= 3; index++ {
		authIndex := fmt.Sprintf("worker-auth-%d", index)
		seedUsageIdentity(t, db, entities.UsageIdentity{Identity: authIndex, Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
		seedUsageEvent(t, db, entities.UsageEvent{AuthType: "oauth", AuthIndex: authIndex, Model: "gpt-5.5", Timestamp: now.Add(-time.Hour), TotalTokens: int64(index)})
	}
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()

	var active atomic.Int64
	var peak atomic.Int64
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	callbackName := "test:block_header_identity_usage_queries"
	if err := db.Callback().Row().Before("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "usage_events" {
			return
		}
		current := active.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
	}); err != nil {
		t.Fatalf("register header worker concurrency callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Row().Remove(callbackName) })

	done := make(chan struct{})
	go func() {
		applyUsageHeaderSnapshots(service, context.Background(), []UsageHeaderSnapshot{
			codexUsageHeaderSnapshot("worker-auth-1", now, "4"),
			codexUsageHeaderSnapshot("worker-auth-2", now, "8"),
			codexUsageHeaderSnapshot("worker-auth-3", now, "12"),
		})
		close(done)
	}()
	for enteredCount := 0; enteredCount < 2; enteredCount++ {
		select {
		case <-entered:
		case <-time.After(500 * time.Millisecond):
			close(release)
			<-done
			t.Fatalf("expected two identity workers to enter usage queries, peak=%d", peak.Load())
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("header identity workers did not finish after release")
	}
	if got := peak.Load(); got != 2 {
		t.Fatalf("expected identity concurrency peak 2, got %d", got)
	}
}

func TestTryAppendUsageHeaderSnapshotsKeepsLatestPendingSnapshotPerAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()
	older := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	newer := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 10, 0, time.Local), "9")

	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(older)) {
		t.Fatal("expected older snapshot append to be accepted")
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(newer)) {
		t.Fatal("expected newer snapshot append to be accepted")
	}
	service.StopRefreshTasks()

	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 9 {
		t.Fatalf("expected latest pending snapshot to win, got %+v", task)
	}
}

func TestUsageHeaderPendingKeepsOneThousandIdentitiesAndStillUpdatesExistingOnes(t *testing.T) {
	// 一分钟内身份种类异常增长时内存必须有硬上限；已接收身份仍允许更新到最新 Header。
	pending := make(map[string]UsageHeaderSnapshot)
	firstBatch := make([]UsageHeaderSnapshot, 0, 1000)
	baseTime := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	for index := 0; index < 1000; index++ {
		firstBatch = append(firstBatch, UsageHeaderSnapshot{
			AuthType: "oauth", AuthIndex: fmt.Sprintf("bounded-auth-%04d", index), Provider: "codex", ObservedAt: baseTime,
		})
	}
	mergePendingUsageHeaderSnapshots(pending, firstBatch)
	newerExisting := UsageHeaderSnapshot{AuthType: "oauth", AuthIndex: "bounded-auth-0000", Provider: "codex", ObservedAt: baseTime.Add(time.Minute)}
	overflow := UsageHeaderSnapshot{AuthType: "oauth", AuthIndex: "bounded-auth-overflow", Provider: "codex", ObservedAt: baseTime.Add(2 * time.Minute)}
	mergePendingUsageHeaderSnapshots(pending, []UsageHeaderSnapshot{newerExisting, overflow})

	if len(pending) != 1000 {
		t.Fatalf("expected pending identity cap 1000, got %d", len(pending))
	}
	if got := pending["bounded-auth-0000"].ObservedAt; !got.Equal(newerExisting.ObservedAt) {
		t.Fatalf("expected existing identity to update at cap, got %s", got)
	}
	if _, ok := pending["bounded-auth-overflow"]; ok {
		t.Fatal("expected a new identity beyond the cap to be rejected")
	}
}

func TestTryAppendUsageHeaderSnapshotsFlushesDifferentAuthIndexesTogether(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth-1", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth-2", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	identityQueries := 0
	priceQueries := 0
	callbackName := "test:count_header_flush_batch_queries"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if queryMentionsTable(sql, "usage_identities") {
			identityQueries++
		}
		if queryMentionsTable(sql, "model_price_settings") {
			priceQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour, PricingCatalog: emptyPricingCatalogForTest()})
	defer service.StopRefreshTasks()

	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(
		codexUsageHeaderSnapshot("codex-auth-1", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"),
		codexUsageHeaderSnapshot("codex-auth-2", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "8"),
	)) {
		t.Fatal("expected snapshot append to be accepted")
	}
	service.StopRefreshTasks()

	first, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth-1")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex auth-1 returned error: %v", err)
	}
	second, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth-2")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex auth-2 returned error: %v", err)
	}
	if first.Quota == nil || len(first.Quota.Quota) != 1 || first.Quota.Quota[0].UsedPercent == nil || *first.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected first auth header quota, got %+v", first)
	}
	if second.Quota == nil || len(second.Quota.Quota) != 1 || second.Quota.Quota[0].UsedPercent == nil || *second.Quota.Quota[0].UsedPercent != 8 {
		t.Fatalf("expected second auth header quota, got %+v", second)
	}
	if identityQueries != 2 {
		t.Fatalf("expected history and cache runners to each batch identity lookup once, got %d", identityQueries)
	}
	if priceQueries != 0 {
		t.Fatalf("expected flush to use cached pricing without DB queries, got %d", priceQueries)
	}
}

func seedUsageEvent(t *testing.T, db *gorm.DB, event entities.UsageEvent) {
	t.Helper()
	if event.EventKey == "" {
		event.EventKey = "event-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
}

func openQuotaReaderPoolTestDatabase(t *testing.T) (*gorm.DB, func()) {
	// Header worker 并发测试需要真实独立 reader；普通 quota 单池夹具无法观测两个并行查询。
	t.Helper()
	db, readDB, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-reader.db")})
	if err != nil {
		t.Fatalf("open quota reader pools: %v", err)
	}
	closePools := func() {
		if readDB != nil && readDB != db {
			if sqlDB, dbErr := readDB.DB(); dbErr == nil {
				_ = sqlDB.Close()
			}
		}
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	}
	return db, closePools
}

func codexUsageHeaderSnapshot(authIndex string, observedAt time.Time, usedPercent string) UsageHeaderSnapshot {
	return codexUsageHeaderSnapshotWithHeaders(authIndex, observedAt, codexUsageHeader(usedPercent))
}

func codexUsageHeaderSnapshotWithHeaders(authIndex string, observedAt time.Time, headers http.Header) UsageHeaderSnapshot {
	// 测试 helper 走真实单次解码入口，避免继续构造已经移除 Header 所有权的旧快照形态。
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{
		AuthType:   "oauth",
		AuthIndex:  authIndex,
		Provider:   "codex",
		ObservedAt: observedAt,
		Headers:    headers,
	})
	if !ok || snapshot == nil {
		panic("expected valid Codex usage Header snapshot")
	}
	return *snapshot
}

func usageHeaderSnapshotPointers(values ...UsageHeaderSnapshot) []*UsageHeaderSnapshot {
	// 每个值在返回切片中拥有稳定地址，模拟生产代码只 fan-out 不可变快照指针的调用形态。
	pointers := make([]*UsageHeaderSnapshot, 0, len(values))
	for index := range values {
		pointers = append(pointers, &values[index])
	}
	return pointers
}

type usageHeaderManualTimer struct {
	delay time.Duration
	fire  chan time.Time
}

func codexUsageHeader(usedPercent string) http.Header {
	return http.Header{
		"X-Codex-Primary-Used-Percent":   []string{usedPercent},
		"X-Codex-Primary-Window-Minutes": []string{"300"},
		"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local).Unix(), 10)},
	}
}

func queryMentionsTable(sql string, table string) bool {
	lowerSQL := strings.ToLower(sql)
	table = strings.ToLower(table)
	return strings.Contains(lowerSQL, "from `"+table+"`") ||
		strings.Contains(lowerSQL, `from "`+table+`"`) ||
		strings.Contains(lowerSQL, "from "+table)
}
