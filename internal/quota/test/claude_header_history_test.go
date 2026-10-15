package test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageHeaderSnapshotDispatchesByOAuthAndProvider(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	headers := http.Header{
		"anthropic-ratelimit-unified-5h-utilization": {"0.25"},
		"anthropic-ratelimit-unified-5h-reset":       {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
		"X-Codex-Primary-Used-Percent":               {"80"},
		"X-Codex-Primary-Window-Minutes":             {"300"},
		"X-Codex-Primary-Reset-At":                   {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
	}
	for _, tc := range []struct {
		provider string
		wantKey  string
		wantUsed float64
		wantOK   bool
	}{
		{provider: " Claude ", wantKey: "five_hour", wantUsed: 25, wantOK: true},
		{provider: "codex", wantKey: "rate_limit.primary_window", wantUsed: 80, wantOK: true},
		{provider: "unknown"},
		{provider: ""},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
				AuthType: " OAUTH ", AuthIndex: " auth ", Provider: tc.provider, ObservedAt: now, Headers: headers,
			})
			if ok != tc.wantOK {
				t.Fatalf("provider %q: snapshot ok=%v, want %v", tc.provider, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			rows := quota.NormalizeQuotaRows(snapshot.CacheOutput)
			if len(rows) != 1 || rows[0].Key != tc.wantKey || rows[0].UsedPercent == nil || *rows[0].UsedPercent != tc.wantUsed {
				t.Fatalf("provider %q: rows=%+v", tc.provider, rows)
			}
			if len(snapshot.MainQuotaObservations) != 1 || snapshot.MainQuotaObservations[0].RemainingPercent != 100-int(tc.wantUsed) {
				t.Fatalf("provider %q: history=%+v", tc.provider, snapshot.MainQuotaObservations)
			}
		})
	}
}

func TestClaudeHeaderMainWindowValidationAndCacheHistoryProjection(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	reset := strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)
	for _, tc := range []struct {
		name, utilization, reset, window string
		wantSnapshot                     bool
		wantCache                        bool
		wantUsed                         float64
		wantRemaining                    int
	}{
		{"zero", "0", reset, "5h", true, true, 0, 100},
		{"negative", "-0.1", reset, "5h", true, false, 0, 100},
		{"above_one", "1.2", reset, "5h", true, true, 120, 0},
		{"seven_only", "0.25", reset, "7d", true, true, 25, 75},
		{"missing", "", reset, "5h", false, false, 0, 0},
		{"nan", "NaN", reset, "5h", false, false, 0, 0},
		{"inf", "+Inf", reset, "5h", false, false, 0, 0},
		{"overflow", "1e308", reset, "5h", false, false, 0, 0},
		{"bad_reset", "0.25", "invalid", "5h", false, false, 0, 0},
		{"zero_reset", "0.25", "0", "5h", false, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{
				"Anthropic-Ratelimit-Unified-" + tc.window + "-Utilization": {tc.utilization},
				"Anthropic-Ratelimit-Unified-" + tc.window + "-Reset":       {tc.reset},
				"Anthropic-Ratelimit-Unified-7d-Sonnet-Utilization":         {"0.99"},
			}
			snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: "claude-auth", Provider: "claude", ObservedAt: now, Headers: headers})
			if ok != tc.wantSnapshot {
				t.Fatalf("snapshot=%v, want %v", ok, tc.wantSnapshot)
			}
			if !ok {
				return
			}
			rows := quota.NormalizeQuotaRows(snapshot.CacheOutput)
			if (len(rows) == 1) != tc.wantCache {
				t.Fatalf("cache rows=%+v, want cache=%v", rows, tc.wantCache)
			}
			if tc.wantCache {
				key := "five_hour"
				if tc.window == "7d" {
					key = "seven_day"
				}
				if rows[0].Key != key || rows[0].UsedPercent == nil || *rows[0].UsedPercent != tc.wantUsed {
					t.Fatalf("cache rows=%+v", rows)
				}
			}
			if len(snapshot.MainQuotaObservations) != 1 || snapshot.MainQuotaObservations[0].RemainingPercent != tc.wantRemaining {
				t.Fatalf("history observations=%+v", snapshot.MainQuotaObservations)
			}
		})
	}
}

func TestClaudeHeaderFanoutUsesRealAuthFileTypeForCacheAndHistory(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	now := time.Now().Truncate(time.Second)
	build := func(authIndex, provider string) *quota.UsageHeaderSnapshot {
		t.Helper()
		snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
			AuthType: "oauth", AuthIndex: authIndex, Provider: provider, ObservedAt: now,
			Headers: http.Header{
				"Anthropic-Ratelimit-Unified-5h-Utilization": {"0.25"},
				"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
				"Anthropic-Ratelimit-Unified-7d-Utilization": {"0"},
				"Anthropic-Ratelimit-Unified-7d-Reset":       {strconv.FormatInt(now.Add(7*24*time.Hour).Unix(), 10)},
			},
		})
		if !ok {
			t.Fatal("expected Claude snapshot")
		}
		return snapshot
	}
	service := quota.NewServiceWithRegistryAndOptions(db, quota.NewProviderRegistry(nil), quota.ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	if !service.TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot{build("claude-auth", "claude"), build("codex-auth", "claude")}) {
		t.Fatal("expected committed usage fanout to accept snapshots")
	}
	service.StopRefreshTasks()
	cache, err := service.GetRefreshTaskByAuthIndex(context.Background(), "claude-auth")
	if err != nil || cache.Quota == nil || len(cache.Quota.Quota) != 2 || cache.Quota.Quota[0].Key != "five_hour" || cache.Quota.Quota[1].Key != "seven_day" {
		t.Fatalf("Claude cache rows: response=%+v err=%v", cache, err)
	}
	if record := refreshTaskRecord(service, "codex-auth"); record != nil {
		t.Fatalf("mismatched Claude header wrote Codex cache: %+v", record)
	}
	var cycles []entities.QuotaCycle
	if err := db.Order("quota_key ASC").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 2 || cycles[0].Provider != "claude" || cycles[0].AuthIndex != "claude-auth" || cycles[0].QuotaKey != "five_hour" || cycles[1].QuotaKey != "seven_day" {
		t.Fatalf("expected only two Claude history cycles, got %+v", cycles)
	}
	response, err := service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "claude-auth", Now: now.Add(time.Minute)})
	if err != nil || len(response.Windows) != 2 || response.SelectedWindow == nil || response.SelectedWindow.WindowRole != "primary" || len(response.Cycles) != 1 || response.Cycles[0].LastRemainingPercent == nil || *response.Cycles[0].LastRemainingPercent != 75 {
		t.Fatalf("Claude 5h history API mapping: response=%+v err=%v", response, err)
	}
	secondary := "secondary"
	response, err = service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "claude-auth", WindowRole: &secondary, Now: now.Add(time.Minute)})
	if err != nil || response.SelectedWindow == nil || response.SelectedWindow.WindowRole != "secondary" || len(response.Cycles) != 1 || response.Cycles[0].LastRemainingPercent == nil || *response.Cycles[0].LastRemainingPercent != 100 {
		t.Fatalf("Claude 7d history API mapping: response=%+v err=%v", response, err)
	}
}

func TestClaudeDeleteClearsOnlyMatchingQueuedWindow(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	now := time.Now().Truncate(time.Second)
	build := func(at time.Time, five, seven string) *quota.UsageHeaderSnapshot {
		t.Helper()
		snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: "claude-auth", Provider: "claude", ObservedAt: at,
			Headers: http.Header{
				"Anthropic-Ratelimit-Unified-5h-Utilization": {five},
				"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
				"Anthropic-Ratelimit-Unified-7d-Utilization": {seven},
				"Anthropic-Ratelimit-Unified-7d-Reset":       {strconv.FormatInt(now.Add(7*24*time.Hour).Unix(), 10)},
			},
		})
		if !ok {
			t.Fatal("expected snapshot")
		}
		return snapshot
	}
	first := build(now, "0.25", "0.05")
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, first.MainQuotaObservations); err != nil {
		t.Fatal(err)
	}
	var fiveCycle entities.QuotaCycle
	if err := db.Where("provider = ? AND quota_key = ?", "claude", "five_hour").Take(&fiveCycle).Error; err != nil {
		t.Fatal(err)
	}
	service := quota.NewServiceWithRegistryAndOptions(db, quota.NewProviderRegistry(nil), quota.ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	if !service.TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot{build(now.Add(time.Minute), "0.30", "0.10")}) {
		t.Fatal("expected queued observation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.DeleteCodexQuotaHistoryCycle(ctx, "claude-auth", fiveCycle.ID); err != nil {
		t.Fatal(err)
	}
	service.StopRefreshTasks()
	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ?", "claude").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 || cycles[0].QuotaKey != "seven_day" {
		t.Fatalf("deleted Claude 5h cycle was recreated or 7d was removed: %+v", cycles)
	}
}

func TestClaudeHeaderDoesNotMergeOldCodexCacheAfterIdentityTypeChanges(t *testing.T) {
	for _, status := range []quota.RefreshTaskStatus{quota.RefreshTaskStatusCompleted, quota.RefreshTaskStatusQueued, quota.RefreshTaskStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "shared-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
			service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil), emptyPricingCatalogForTest())
			defer service.StopRefreshTasks()
			now := time.Now().Truncate(time.Second)
			credits := 2
			oldUsed := 90.0
			refreshTasks(service)["shared-auth"] = &quota.RefreshTaskRecord{
				AuthIndex: "shared-auth", Type: "codex", Status: status, RefreshedAt: now.Add(time.Minute),
				Quota: &quota.CheckResponse{
					ID: "shared-auth", RateLimitResetCreditsAvailableCount: &credits,
					Subscription: &quota.SubscriptionInfo{Provider: "codex", Plan: "plus"},
					Quota:        []quota.QuotaRow{{Key: "rate_limit.primary_window", UsedPercent: &oldUsed}},
				},
			}
			snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
				AuthType: "oauth", AuthIndex: "shared-auth", Provider: "claude", ObservedAt: now,
				Headers: http.Header{
					"Anthropic-Ratelimit-Unified-5h-Utilization": {"0.25"},
					"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
				},
			})
			if !ok || !applyUsageHeaderSnapshot(service, context.Background(), *snapshot) {
				t.Fatal("expected valid Claude cache update")
			}
			updated := refreshTaskRecord(service, "shared-auth")
			if updated == nil || updated.Type != "claude" || updated.Quota == nil || len(updated.Quota.Quota) != 1 || updated.Quota.Quota[0].Key != "five_hour" || updated.Quota.Subscription != nil || updated.Quota.RateLimitResetCreditsAvailableCount != nil {
				t.Fatalf("old Codex cache leaked into Claude cache: %+v", updated)
			}
		})
	}
}

func TestClaudeActiveCheckSourcesWriteTrustedHistory(t *testing.T) {
	for _, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled, quota.RefreshSourceInspection} {
		t.Run(string(source), func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
			reset := time.Now().Add(5 * time.Hour).Truncate(time.Second)
			handler := &recordingProviderHandler{output: quota.ProviderOutput{Provider: "claude", Result: quota.ClaudeResult{Usage: &quota.ClaudeUsagePayload{
				FiveHour: &quota.ClaudeUsageWindow{Utilization: 0, HasUtilization: true, ResetsAt: reset.Format(time.RFC3339)},
				SevenDay: &quota.ClaudeUsageWindow{Utilization: 25, HasUtilization: false, ResetsAt: reset.Add(7 * 24 * time.Hour).Format(time.RFC3339)},
			}}}}
			service := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": handler}), emptyPricingCatalogForTest())
			response, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "claude-auth", Source: source})
			if err != nil || len(response.Quota) != 2 {
				t.Fatalf("active refresh failed: response=%+v err=%v", response, err)
			}
			service.StopRefreshTasks()
			var cycles []entities.QuotaCycle
			if err := db.Find(&cycles).Error; err != nil {
				t.Fatal(err)
			}
			if len(cycles) != 1 || cycles[0].Provider != "claude" || cycles[0].QuotaKey != "five_hour" {
				t.Fatalf("expected only explicit-zero Claude primary observation, got %+v", cycles)
			}
			var segment entities.QuotaPercentSegment
			if err := db.Where("cycle_id = ?", cycles[0].ID).Take(&segment).Error; err != nil {
				t.Fatal(err)
			}
			if segment.RemainingPercent != 100 {
				t.Fatalf("explicit zero became %+v", segment)
			}
		})
	}
}

func TestClaudePendingMainGroupDoesNotOverwriteNewerActiveSevenDayCache(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := quota.NewServiceWithRegistryAndOptions(db, quota.NewProviderRegistry(nil), quota.ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour, CodexQuotaHistoryFlushInterval: time.Hour,
		PricingCatalog: emptyPricingCatalogForTest(),
	})
	now := time.Now().Truncate(time.Second)
	t1, t2, t3 := now.Add(-2*time.Minute), now.Add(-time.Minute), now
	activeFive, activeSeven := 20.0, 40.0
	refreshTasks(service)["claude-auth"] = &quota.RefreshTaskRecord{
		AuthIndex: "claude-auth", Type: "claude", Status: quota.RefreshTaskStatusCompleted, Source: quota.RefreshSourceManual, RefreshedAt: t2,
		Quota: &quota.CheckResponse{ID: "claude-auth", Quota: []quota.QuotaRow{
			{Key: "five_hour", Scope: "window", UsedPercent: &activeFive},
			{Key: "seven_day", Scope: "window", UsedPercent: &activeSeven},
		}},
	}
	build := func(at time.Time, window, utilization string) *quota.UsageHeaderSnapshot {
		t.Helper()
		prefix := "Anthropic-Ratelimit-Unified-" + window + "-"
		snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
			AuthType: "oauth", AuthIndex: "claude-auth", Provider: "claude", ObservedAt: at,
			Headers: http.Header{prefix + "Utilization": {utilization}, prefix + "Reset": {strconv.FormatInt(now.Add(7*24*time.Hour).Unix(), 10)}},
		})
		if !ok {
			t.Fatal("expected partial Claude snapshot")
		}
		return snapshot
	}
	if !service.TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot{build(t1, "7d", "0.10"), build(t3, "5h", "0.30")}) {
		t.Fatal("expected partial Header snapshots")
	}
	service.StopRefreshTasks()
	record := refreshTaskRecord(service, "claude-auth")
	if record == nil || record.Quota == nil || len(record.Quota.Quota) != 2 {
		t.Fatalf("unexpected merged cache: %+v", record)
	}
	if record.Quota.Quota[0].UsedPercent == nil || *record.Quota.Quota[0].UsedPercent != 30 || record.Quota.Quota[1].UsedPercent == nil || *record.Quota.Quota[1].UsedPercent != 40 {
		t.Fatalf("t1 pending 7d was stitched onto t3 after t2 active refresh: %+v", record.Quota.Quota)
	}
}

func TestClaudePendingPartialWindowsMergeIntoCacheAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first claudePartialHeader
		next  claudePartialHeader
	}{
		{
			name:  "five_then_newer_seven",
			first: claudePartialHeader{atOffset: 0, window: "5h", utilization: "0.10"},
			next:  claudePartialHeader{atOffset: time.Second, window: "7d", utilization: "0.20"},
		},
		{
			name:  "seven_then_older_five",
			first: claudePartialHeader{atOffset: time.Second, window: "7d", utilization: "0.20"},
			next:  claudePartialHeader{atOffset: 0, window: "5h", utilization: "0.10"},
		},
		{
			name:  "same_observed_at_disjoint_windows",
			first: claudePartialHeader{atOffset: 0, window: "5h", utilization: "0.10"},
			next:  claudePartialHeader{atOffset: 0, window: "7d", utilization: "0.20"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
			service := quota.NewServiceWithRegistryAndOptions(db, quota.NewProviderRegistry(nil), quota.ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})
			now := time.Now().Truncate(time.Second)
			first := buildClaudePartialHeaderSnapshot(t, now, tc.first)
			next := buildClaudePartialHeaderSnapshot(t, now, tc.next)
			firstRowsBefore := quota.NormalizeQuotaRows(first.CacheOutput)
			nextRowsBefore := quota.NormalizeQuotaRows(next.CacheOutput)

			if !service.TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot{first, next}) {
				t.Fatal("expected partial Claude Header snapshots")
			}
			service.StopRefreshTasks()

			record := refreshTaskRecord(service, "claude-auth")
			assertClaudeCacheRows(t, record, 10, 20)
			assertClaudeHistorySegments(t, db, map[string]int{"five_hour": 90, "seven_day": 80})
			if rows := quota.NormalizeQuotaRows(first.CacheOutput); !sameQuotaRows(rows, firstRowsBefore) {
				t.Fatalf("first input snapshot was mutated: before=%+v after=%+v", firstRowsBefore, rows)
			}
			if rows := quota.NormalizeQuotaRows(next.CacheOutput); !sameQuotaRows(rows, nextRowsBefore) {
				t.Fatalf("second input snapshot was mutated: before=%+v after=%+v", nextRowsBefore, rows)
			}
		})
	}
}

func TestClaudePendingPartialWindowKeepsNewerSameWindow(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := quota.NewServiceWithRegistryAndOptions(db, quota.NewProviderRegistry(nil), quota.ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	now := time.Now().Truncate(time.Second)
	newerFive := buildClaudePartialHeaderSnapshot(t, now, claudePartialHeader{atOffset: time.Second, window: "5h", utilization: "0.10"})
	newestSeven := buildClaudePartialHeaderSnapshot(t, now, claudePartialHeader{atOffset: 2 * time.Second, window: "7d", utilization: "0.20"})
	staleFive := buildClaudePartialHeaderSnapshot(t, now, claudePartialHeader{atOffset: 0, window: "5h", utilization: "0.90"})

	if !service.TryAppendUsageHeaderSnapshots([]*quota.UsageHeaderSnapshot{newerFive, newestSeven, staleFive}) {
		t.Fatal("expected partial Claude Header snapshots")
	}
	service.StopRefreshTasks()

	record := refreshTaskRecord(service, "claude-auth")
	assertClaudeCacheRows(t, record, 10, 20)
}

type claudePartialHeader struct {
	atOffset    time.Duration
	window      string
	utilization string
}

func buildClaudePartialHeaderSnapshot(t *testing.T, base time.Time, input claudePartialHeader) *quota.UsageHeaderSnapshot {
	t.Helper()
	prefix := "Anthropic-Ratelimit-Unified-" + input.window + "-"
	snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{
		AuthType: "oauth", AuthIndex: "claude-auth", Provider: "claude", ObservedAt: base.Add(input.atOffset),
		Headers: http.Header{
			prefix + "Utilization": {input.utilization},
			prefix + "Reset":       {strconv.FormatInt(base.Add(7*24*time.Hour).Unix(), 10)},
		},
	})
	if !ok {
		t.Fatalf("expected Claude %s snapshot", input.window)
	}
	return snapshot
}

func assertClaudeCacheRows(t *testing.T, record *quota.RefreshTaskRecord, wantFive float64, wantSeven float64) {
	t.Helper()
	if record == nil || record.Quota == nil || len(record.Quota.Quota) != 2 {
		t.Fatalf("unexpected merged cache: %+v", record)
	}
	got := map[string]float64{}
	for _, row := range record.Quota.Quota {
		if row.UsedPercent != nil {
			got[row.Key] = *row.UsedPercent
		}
	}
	if got["five_hour"] != wantFive || got["seven_day"] != wantSeven {
		t.Fatalf("merged cache rows=%+v, want five=%v seven=%v", record.Quota.Quota, wantFive, wantSeven)
	}
}

func assertClaudeHistorySegments(t *testing.T, db *gorm.DB, want map[string]int) {
	t.Helper()
	var cycles []entities.QuotaCycle
	if err := db.Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, cycle := range cycles {
		var segment entities.QuotaPercentSegment
		if err := db.Where("cycle_id = ?", cycle.ID).Take(&segment).Error; err != nil {
			t.Fatal(err)
		}
		got[cycle.QuotaKey] = segment.RemainingPercent
	}
	for key, remaining := range want {
		if got[key] != remaining {
			t.Fatalf("history segments=%+v, want %s=%d", got, key, remaining)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("history segments=%+v, want %+v", got, want)
	}
}

func sameQuotaRows(a, b []quota.QuotaRow) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index].Key != b[index].Key || a[index].Scope != b[index].Scope {
			return false
		}
		if (a[index].UsedPercent == nil) != (b[index].UsedPercent == nil) {
			return false
		}
		if a[index].UsedPercent != nil && *a[index].UsedPercent != *b[index].UsedPercent {
			return false
		}
	}
	return true
}
