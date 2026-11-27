package test

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func openQuotaAggregationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-agg.db")})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func insertQuotaTestEvent(t *testing.T, db *gorm.DB, provider, authType, authIndex string, ts time.Time, failed bool, tokens int64, cost float64, costAvailable bool) int64 {
	t.Helper()
	event := entities.UsageEvent{
		EventKey:            ts.Format(time.RFC3339Nano) + "-" + authIndex,
		APIGroupKey:         "key-1",
		Provider:            provider,
		AuthType:            authType,
		AuthIndex:           authIndex,
		Model:               "gpt-5",
		Timestamp:           ts,
		Failed:              failed,
		InputTokens:         tokens / 2,
		OutputTokens:        tokens / 4,
		ReasoningTokens:     tokens / 8,
		CacheReadTokens:     tokens / 16,
		CacheCreationTokens: tokens / 16,
		TotalTokens:         tokens,
		CostUSD:             &cost,
		CostAvailable:       &costAvailable,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("insert test usage event: %v", err)
	}
	return event.ID
}

func TestAdvanceQuotaHistoryDirtyCursorMarksMatchingCyclesAndAdvancesCheckpoint(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	// 创建同一账号的 primary (5h) 和 secondary (7d) 周期，以及另一账号周期。
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-a",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          now.Add(2 * time.Hour),
			RemainingPercent: 90,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-a",
			WindowRole:       "secondary",
			WindowSeconds:    7 * 24 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          now.Add(5 * 24 * time.Hour),
			RemainingPercent: 80,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-b",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          now.Add(2 * time.Hour),
			RemainingPercent: 100,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write observation: %v", err)
		}
	}
	clearQuotaCyclesDirty(t, db)

	// 插入 1 条命中 auth-a 双窗口的事件，1 条非 oauth 事件（应忽略），1 条 gemini 事件（应忽略）。
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-a", now.Add(-30*time.Minute), false, 1600, 0.25, true)
	insertQuotaTestEvent(t, db, "codex", "api_key", "auth-b", now.Add(-20*time.Minute), false, 1600, 0.25, true)
	lastID := insertQuotaTestEvent(t, db, "gemini", "oauth", "auth-b", now.Add(-10*time.Minute), false, 1600, 0.25, true)

	res, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100)
	if err != nil {
		t.Fatalf("advance dirty cursor: %v", err)
	}
	if res.ScannedEvents != 3 || res.NextCursor != lastID || res.DirtyCyclesMarked != 2 || res.HasMore {
		t.Fatalf("unexpected dirty cursor scan result: %+v", res)
	}

	cursor, err := repository.LoadQuotaHistoryAggregationCheckpoint(ctx, db)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if cursor != lastID {
		t.Fatalf("expected checkpoint %d, got %d", lastID, cursor)
	}

	var cycles []entities.QuotaCycle
	if err := db.Order("id ASC").Find(&cycles).Error; err != nil {
		t.Fatalf("load cycles: %v", err)
	}
	for _, cycle := range cycles {
		wantDirty := cycle.AuthIndex == "auth-a"
		if cycle.Dirty != wantDirty {
			t.Fatalf("cycle %d (%s/%s) dirty=%t, want %t", cycle.ID, cycle.AuthIndex, cycle.QuotaKey, cycle.Dirty, wantDirty)
		}
	}
}

func TestComputeAndApplyQuotaCyclePersistedUsageFullContract(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	// 已结束周期：[now-6h, now-1h)，首观测 83% @ now-5h，降档 79% @ now-3h。
	resetAt := now.Add(-1 * time.Hour)
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-1",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 83,
			FirstObservedAt:  now.Add(-5 * time.Hour),
			LastObservedAt:   now.Add(-5 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-1",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 79,
			FirstObservedAt:  now.Add(-3 * time.Hour),
			LastObservedAt:   now.Add(-3 * time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write observation: %v", err)
		}
	}

	// 1. 首观测前事件（计入周期，不计入下降段）
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-1", now.Add(-5*time.Hour-30*time.Minute), false, 1600, 0.10, true)
	// 2. 83% -> 79% 区间内事件（计入周期与 79% 下降段，含一条失败缺价事件）
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-1", now.Add(-4*time.Hour), false, 3200, 0.25, true)
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-1", now.Add(-3*time.Hour), true, 1600, 0.0, false)
	// 3. 尾观测后事件（计入周期，不计入下降段）
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-1", now.Add(-2*time.Hour), false, 1600, 0.15, true)

	// 同账号再建一个 0 请求周期（在 90 天热表保留期内）和一个超 90 天归档线的老周期。
	zeroReset := now.Add(-10 * 24 * time.Hour)
	oldReset := now.Add(-100 * 24 * time.Hour)
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-zero",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          zeroReset,
			RemainingPercent: 100,
			FirstObservedAt:  zeroReset.Add(-time.Hour),
			LastObservedAt:   zeroReset.Add(-time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-old",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          oldReset,
			RemainingPercent: 95,
			FirstObservedAt:  oldReset.Add(-time.Hour),
			LastObservedAt:   oldReset.Add(-time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write extra observation: %v", err)
		}
	}

	// 验证对主周期设置 cost_stale=true 后，日常重算不能清除 cost_stale。
	if err := db.Model(&entities.QuotaCycle{}).Where("auth_index = ?", "auth-1").Update("cost_stale", true).Error; err != nil {
		t.Fatalf("set cost_stale: %v", err)
	}

	cycles, _, err := repository.SelectQuotaCyclesForUsageSync(ctx, db, now, 10, nil)
	if err != nil {
		t.Fatalf("select cycles: %v", err)
	}
	if len(cycles) != 3 {
		t.Fatalf("expected 3 dirty cycles, got %d", len(cycles))
	}

	applied, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, cycles, now, 0, false)
	if err != nil {
		t.Fatalf("recompute cycles: %v", err)
	}
	if applied != 3 {
		t.Fatalf("expected 3 cycles applied, got %d", applied)
	}

	// 核对主周期 11 项字段与 cost_stale 保留。
	var mainCycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-1").Take(&mainCycle).Error; err != nil {
		t.Fatalf("load main cycle: %v", err)
	}
	if !mainCycle.UsageReady || mainCycle.Dirty || !mainCycle.CostStale {
		t.Fatalf("unexpected main cycle flags: ready=%t dirty=%t cost_stale=%t", mainCycle.UsageReady, mainCycle.Dirty, mainCycle.CostStale)
	}
	if mainCycle.Requests != 4 || mainCycle.SuccessfulRequests != 3 || mainCycle.FailedRequests != 1 ||
		mainCycle.TotalTokens != 8000 || mainCycle.UnavailableCostCount != 1 || math.Abs(mainCycle.CostUSD-0.50) > 1e-9 {
		t.Fatalf("unexpected main cycle totals: %+v", mainCycle)
	}

	var segs []entities.QuotaPercentSegment
	if err := db.Where("cycle_id = ?", mainCycle.ID).Order("first_observed_at ASC").Find(&segs).Error; err != nil {
		t.Fatalf("load segments: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segs))
	}
	if segs[0].TransitionUsageReady || segs[0].TransitionRequests != 0 {
		t.Fatalf("first segment must keep transition_usage_ready=false: %+v", segs[0])
	}
	if !segs[1].TransitionUsageReady || segs[1].TransitionRequests != 2 || segs[1].TransitionFailedRequests != 1 ||
		segs[1].TransitionTotalTokens != 4800 || segs[1].TransitionUnavailableCostCount != 1 || math.Abs(segs[1].TransitionCostUSD-0.25) > 1e-9 {
		t.Fatalf("unexpected drop segment totals: %+v", segs[1])
	}

	// 核对 90 天内真实 0 请求周期正常结算为 usage_ready=true。
	var zeroCycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-zero").Take(&zeroCycle).Error; err != nil {
		t.Fatalf("load zero cycle: %v", err)
	}
	if !zeroCycle.UsageReady || zeroCycle.Dirty || zeroCycle.Requests != 0 || zeroCycle.TotalTokens != 0 {
		t.Fatalf("expected zero-usage cycle within hot retention to settle ready=true: %+v", zeroCycle)
	}

	// 核对超 90 天归档线的老周期清除 dirty 且保留 usage_ready=false，不再被后续选中。
	var oldCycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-old").Take(&oldCycle).Error; err != nil {
		t.Fatalf("load old cycle: %v", err)
	}
	if oldCycle.UsageReady || oldCycle.Dirty {
		t.Fatalf("expected beyond-retention cycle to clear dirty and stay usage_ready=false: %+v", oldCycle)
	}
	remaining, hasMore, err := repository.SelectQuotaCyclesForUsageSync(ctx, db, now, 10, nil)
	if err != nil {
		t.Fatalf("select remaining cycles: %v", err)
	}
	if len(remaining) != 0 || hasMore {
		t.Fatalf("expected no remaining cycles after sync, got %d hasMore=%t", len(remaining), hasMore)
	}
}

func TestApplyComputedQuotaCycleUsageRespectsBoundaryAndDirtyVersions(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-ver",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 90,
		FirstObservedAt:  now.Add(-time.Hour),
		LastObservedAt:   now.Add(-time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write observation: %v", err)
	}
	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-ver").Take(&cycle).Error; err != nil {
		t.Fatalf("load cycle: %v", err)
	}

	computed, err := repository.ComputeQuotaCyclePersistedUsage(ctx, db, cycle, now, 0)
	if err != nil {
		t.Fatalf("compute cycle: %v", err)
	}

	// 场景 A：计算期间有新事件到达（仅 dirty_version 递增，boundary_version 未变），回写应落库但保留 dirty=true。
	if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).
		Update("dirty_version", gorm.Expr("dirty_version + 1")).Error; err != nil {
		t.Fatalf("bump dirty_version: %v", err)
	}
	applied, err := repository.ApplyComputedQuotaCycleUsage(ctx, db, computed, now, false)
	if err != nil {
		t.Fatalf("apply with newer dirty_version: %v", err)
	}
	if !applied {
		t.Fatal("expected apply to succeed when boundary_version matches")
	}
	reloaded := loadQuotaCycleByID(t, db, cycle.ID)
	if !reloaded.UsageReady || !reloaded.Dirty {
		t.Fatalf("expected usage_ready=true and dirty=true when dirty_version advanced: %+v", reloaded)
	}

	// 场景 B：计算期间边界变更（boundary_version 递增），回写必须放弃。
	if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).
		Update("boundary_version", gorm.Expr("boundary_version + 1")).Error; err != nil {
		t.Fatalf("bump boundary_version: %v", err)
	}
	computed.Usage.Requests = 999
	applied, err = repository.ApplyComputedQuotaCycleUsage(ctx, db, computed, now, false)
	if err != nil {
		t.Fatalf("apply with stale boundary_version: %v", err)
	}
	if applied {
		t.Fatal("expected apply to be rejected when boundary_version changed")
	}
	reloaded = loadQuotaCycleByID(t, db, cycle.ID)
	if reloaded.Requests == 999 {
		t.Fatalf("stale boundary_version write should not overwrite requests: %+v", reloaded)
	}
}

func TestAdvanceQuotaHistoryDirtyCursorSkipsNullableLegacyIdentityColumns(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-null-safe",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 90,
		FirstObservedAt:  now.Add(-time.Hour),
		LastObservedAt:   now.Add(-time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write observation: %v", err)
	}
	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-null-safe").Take(&cycle).Error; err != nil {
		t.Fatalf("load cycle: %v", err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, []entities.QuotaCycle{cycle}, now, 0, false); err != nil {
		t.Fatalf("initial recompute: %v", err)
	}

	// 插入一条早期迁移遗留 provider/auth_type/auth_index 均为 NULL 的旧记录，紧跟一条有效 Codex OAuth 请求。
	if err := db.Exec(`INSERT INTO usage_events
		(event_key, api_group_key, provider, endpoint, auth_type, request_id, session_id, model, timestamp, source, auth_index, failed, total_tokens, cost_usd, cost_available, created_at)
		VALUES ('legacy-null-row', 'g1', NULL, '/v1/chat/completions', NULL, 'req-null', 's1', 'gpt-4', ?, 'legacy', NULL, 0, 100, 0.01, 1, ?)`,
		now.Add(-30*time.Minute).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)).Error; err != nil {
		t.Fatalf("insert nullable legacy usage event: %v", err)
	}
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-null-safe", now.Add(-15*time.Minute), false, 800, 0.12, true)

	scanRes, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100)
	if err != nil {
		t.Fatalf("AdvanceQuotaHistoryDirtyCursor failed on NULL legacy columns: %v", err)
	}
	if scanRes.ScannedEvents != 2 || scanRes.DirtyCyclesMarked != 1 {
		t.Fatalf("unexpected scan result: %+v", scanRes)
	}
	updated := loadQuotaCycleByID(t, db, cycle.ID)
	if !updated.Dirty {
		t.Fatalf("expected valid event after NULL legacy row to mark cycle dirty: %+v", updated)
	}
}
