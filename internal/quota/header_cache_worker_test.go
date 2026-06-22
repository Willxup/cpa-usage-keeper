package quota

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"

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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	applied := service.applyUsageHeaderSnapshot(context.Background(), UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		Headers: http.Header{
			"X-Codex-Plan-Type":              []string{"pro"},
			"X-Codex-Primary-Used-Percent":   []string{"4"},
			"X-Codex-Primary-Window-Minutes": []string{"300"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(time.Date(2026, 6, 22, 15, 0, 0, 0, time.Local).Unix(), 10)},
		},
	})
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
	row := task.Quota.Quota[0]
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 123 || row.WindowUsageCost == nil {
		t.Fatalf("expected local token/cost fallback, got %#v", row)
	}
	if task.RefreshedAt == nil || !task.RefreshedAt.Equal(time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)) {
		t.Fatalf("expected refreshed_at from observed_at, got %+v", task.RefreshedAt)
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	applied := service.applyUsageHeaderSnapshot(context.Background(), UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: observedAt,
		Headers: http.Header{
			"X-Codex-Plan-Type":              []string{"pro"},
			"X-Codex-Primary-Used-Percent":   []string{"4"},
			"X-Codex-Primary-Window-Minutes": []string{"300"},
			"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(observedAt.Add(4*time.Hour).Unix(), 10)},
		},
	})
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	snapshot := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	snapshot.Provider = "claude"
	applied := service.applyUsageHeaderSnapshot(context.Background(), snapshot)
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("claude-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if applied {
		t.Fatal("expected non-codex usage identity type to ignore codex-looking headers")
	}
	if len(service.refreshTasks) != 0 {
		t.Fatalf("expected no quota cache task, got %+v", service.refreshTasks)
	}
}

func TestApplyUsageHeaderSnapshotSkipsActiveRefreshTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{AuthIndex: "codex-auth", Status: RefreshTaskStatusQueued, Source: RefreshSourceManual}

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if applied {
		t.Fatal("expected active refresh task to win over header snapshot")
	}
	if task := service.refreshTasks["codex-auth"]; task.Status != RefreshTaskStatusQueued || task.Quota != nil {
		t.Fatalf("expected queued task to remain unchanged, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotUpdatesRecentCompletedCacheAndCreatesMissingCache(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "new-codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: refreshedAt,
		Quota:       &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", Scope: "window", UsedPercent: floatPtr(90)}}},
	}

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected recent newer header to update completed cache")
	}
	task := service.refreshTasks["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected recent header progress to update cache, got %+v", task)
	}

	applied = service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("new-codex-auth", refreshedAt.Add(20*time.Second), "8"))
	if !applied {
		t.Fatal("expected missing cache to be created despite debounce window")
	}
	created := service.refreshTasks["new-codex-auth"]
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
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
	callbackName := "test:count_header_recent_update_window_stats_queries"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if queryMentionsTable(sql, "usage_events") || queryMentionsTable(sql, "usage_overview_hourly_stats") || queryMentionsTable(sql, "model_price_settings") {
			windowStatsQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected recent newer header to update completed cache")
	}
	if windowStatsQueries == 0 {
		t.Fatal("expected recent header update to refresh window stats")
	}
	task := service.refreshTasks["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected recent header progress to update cache, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotWarnsOnIdentityDatabaseError(t *testing.T) {
	hook := logrustest.NewGlobal()
	defer hook.Reset()
	previousLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() { logrus.SetLevel(previousLevel) })

	service := NewServiceWithRegistry(nil, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	if service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Now(), "4")) {
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	httpStatus := 429
	refreshedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:      "codex-auth",
		Status:         RefreshTaskStatusFailed,
		Error:          "rate limited",
		HTTPStatusCode: &httpStatus,
		Source:         RefreshSourceManual,
		RefreshedAt:    refreshedAt,
		ExpiresAt:      time.Now().Add(time.Hour),
		Quota:          &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", Scope: "window", UsedPercent: floatPtr(90)}}},
	}

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", refreshedAt.Add(20*time.Second), "4"))
	if !applied {
		t.Fatal("expected failed cache to be recovered by complete header inside debounce window")
	}
	task := service.refreshTasks["codex-auth"]
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	newerAt := time.Date(2026, 6, 22, 12, 0, 0, 0, time.Local)
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: newerAt,
		Quota:       &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", UsedPercent: floatPtr(90)}}},
	}

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", newerAt.Add(-time.Hour), "4"))
	if applied {
		t.Fatal("expected older header snapshot to be ignored")
	}
	task := service.refreshTasks["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 90 {
		t.Fatalf("expected newer cache to remain unchanged, got %+v", task)
	}
}

func TestApplyUsageHeaderSnapshotIgnoresIncompleteWindowWithoutClearingExistingUsage(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	oldPercent := 80.0
	oldTokens := int64(999)
	oldCost := 9.9
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
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

	applied := service.applyUsageHeaderSnapshot(context.Background(), UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		Headers: http.Header{
			"X-Codex-Primary-Used-Percent": []string{"4"},
		},
	})
	if applied {
		t.Fatal("expected incomplete header window to be ignored")
	}
	task := service.refreshTasks["codex-auth"]
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	oldUsed := 8.0
	oldLimit := 10.0
	oldRemaining := 2.0
	oldRemainingFraction := 0.2
	oldPercent := 80.0
	oldTokens := int64(999)
	oldCost := 9.9
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
		AuthIndex:   "codex-auth",
		Status:      RefreshTaskStatusCompleted,
		Source:      RefreshSourceManual,
		RefreshedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.Local),
		Quota: &CheckResponse{ID: "codex-auth", Quota: []QuotaRow{{
			Key:               "rate_limit.primary_window",
			Label:             "Manual 5h",
			Scope:             "window",
			Metric:            "manual",
			PlanType:          "pro",
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

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if !applied {
		t.Fatal("expected header snapshot to update stale cache")
	}
	task := service.refreshTasks["codex-auth"]
	if task.Quota == nil || len(task.Quota.Quota) != 1 {
		t.Fatalf("unexpected merged task: %+v", task)
	}
	row := task.Quota.Quota[0]
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

func TestApplyUsageHeaderSnapshotMergesRowsAndPreservesResetCredits(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()
	credits := 2
	oldPercent := 61.0
	service.refreshTasks["codex-auth"] = &RefreshTaskRecord{
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

	applied := service.applyUsageHeaderSnapshot(context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"))
	if !applied {
		t.Fatal("expected newer header snapshot to apply")
	}
	task := service.refreshTasks["codex-auth"]
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	applied := service.applyUsageHeaderSnapshot(context.Background(), UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local),
		Headers: http.Header{
			"X-Codex-Bengalfox-Limit-Name":                  []string{"GPT-5.3-Codex-Spark"},
			"X-Codex-Bengalfox-Primary-Used-Percent":        []string{"5"},
			"X-Codex-Bengalfox-Primary-Window-Minutes":      []string{"300"},
			"X-Codex-Bengalfox-Primary-Reset-After-Seconds": []string{"60"},
		},
	})
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": nil}))
	defer service.StopRefreshTasks()

	tests := []UsageHeaderSnapshot{
		{AuthType: "apikey", AuthIndex: "codex-auth", Provider: "codex", ObservedAt: time.Now(), Headers: codexUsageHeader("4")},
		{AuthType: "oauth", Provider: "codex", ObservedAt: time.Now(), Headers: codexUsageHeader("4")},
		{AuthType: "oauth", AuthIndex: "provider-auth", Provider: "codex", ObservedAt: time.Now(), Headers: codexUsageHeader("4")},
		{AuthType: "oauth", AuthIndex: "deleted-auth", Provider: "codex", ObservedAt: time.Now(), Headers: codexUsageHeader("4")},
		{AuthType: "oauth", AuthIndex: "claude-auth", Provider: "codex", ObservedAt: time.Now(), Headers: codexUsageHeader("4")},
		{AuthType: "oauth", AuthIndex: "codex-auth", Provider: "codex", ObservedAt: time.Now(), Headers: http.Header{"X-Codex-Credits-Has-Credits": []string{"False"}}},
	}
	for _, snapshot := range tests {
		if service.applyUsageHeaderSnapshot(context.Background(), snapshot) {
			t.Fatalf("expected snapshot to be ignored: %+v", snapshot)
		}
	}
	if len(service.refreshTasks) != 0 {
		t.Fatalf("expected no header cache tasks, got %+v", service.refreshTasks)
	}
}

func TestStopRefreshTasksStopsUsageHeaderWorker(t *testing.T) {
	service := NewServiceWithRegistry(openQuotaTestDatabase(t), NewProviderRegistry(nil))
	service.StopRefreshTasks()

	if service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{codexUsageHeaderSnapshot("codex-auth", time.Now(), "4")}) {
		t.Fatal("expected stopped usage header worker to reject new snapshots")
	}
}

func TestTryAppendUsageHeaderSnapshotsWaitsForFlushBeforeApplyingOrQueryingIdentity(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour})
	defer service.StopRefreshTasks()
	identityQueries := 0
	callbackName := "test:count_header_flush_identity_queries"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if queryMentionsTable(tx.Statement.SQL.String(), "usage_identities") {
			identityQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	snapshot := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	if !service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{snapshot}) {
		t.Fatal("expected snapshot append to be accepted")
	}
	snapshot.Headers.Set("X-Codex-Primary-Used-Percent", "99")

	time.Sleep(30 * time.Millisecond)
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); err == nil {
		t.Fatal("expected snapshot to remain pending before flush interval")
	}
	if identityQueries != 0 {
		t.Fatalf("expected no identity query before header flush, got %d", identityQueries)
	}

	service.StopRefreshTasks()
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected stopped worker to flush cloned header snapshot, got %+v", task)
	}
}

func TestTryAppendUsageHeaderSnapshotsFlushesPendingSnapshotsOnInterval(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: 20 * time.Millisecond})
	defer service.StopRefreshTasks()

	if !service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")}) {
		t.Fatal("expected snapshot append to be accepted")
	}

	task := waitForRefreshTask(t, service, "codex-auth", RefreshTaskStatusCompleted)
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 4 {
		t.Fatalf("expected interval flush to apply header snapshot, got %+v", task)
	}
}

func TestTryAppendUsageHeaderSnapshotsKeepsLatestPendingSnapshotPerAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour})
	defer service.StopRefreshTasks()
	older := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4")
	newer := codexUsageHeaderSnapshot("codex-auth", time.Date(2026, 6, 22, 11, 0, 10, 0, time.Local), "9")

	if !service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{older}) {
		t.Fatal("expected older snapshot append to be accepted")
	}
	if !service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{newer}) {
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
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour})
	defer service.StopRefreshTasks()

	if !service.TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot{
		codexUsageHeaderSnapshot("codex-auth-1", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "4"),
		codexUsageHeaderSnapshot("codex-auth-2", time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local), "8"),
	}) {
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
	if identityQueries != 1 {
		t.Fatalf("expected flush to batch identity lookup into 1 query, got %d", identityQueries)
	}
	if priceQueries != 1 {
		t.Fatalf("expected flush to reuse one price settings query, got %d", priceQueries)
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

func codexUsageHeaderSnapshot(authIndex string, observedAt time.Time, usedPercent string) UsageHeaderSnapshot {
	return UsageHeaderSnapshot{
		AuthType:   "oauth",
		AuthIndex:  authIndex,
		Provider:   "codex",
		ObservedAt: observedAt,
		Headers:    codexUsageHeader(usedPercent),
	}
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
