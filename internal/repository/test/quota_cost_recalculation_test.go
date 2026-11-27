package test

import (
	"context"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func TestApplyUsageCostRecalculationBatchMarksQuotaCyclesCostStaleAndRecomputes(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	// 创建 1 个在 90 天热表保留期内的周期与 1 个超 90 天的陈旧周期。
	resetAt := now.Add(-1 * time.Hour)
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-price",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 90,
			FirstObservedAt:  now.Add(-5 * time.Hour),
			LastObservedAt:   now.Add(-5 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-price",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 85,
			FirstObservedAt:  now.Add(-3 * time.Hour),
			LastObservedAt:   now.Add(-3 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-stale-old",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          now.Add(-100 * 24 * time.Hour),
			RemainingPercent: 80,
			FirstObservedAt:  now.Add(-100*24*time.Hour - time.Hour),
			LastObservedAt:   now.Add(-100*24*time.Hour - time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write observation: %v", err)
		}
	}

	// 插入 1 条在下降段内的 Codex OAuth 事件（旧费用 0.10）。
	evTime := now.Add(-4 * time.Hour)
	evID := insertQuotaTestEvent(t, db, "codex", "oauth", "auth-price", evTime, false, 1_000_000, 0.10, true)

	// 先结算初始持久化统计，并补齐 Overview 小时/日桶以便 ApplyUsageCostRecalculationBatch 扣差额。
	var mainCycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-price").Take(&mainCycle).Error; err != nil {
		t.Fatalf("load main cycle: %v", err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, []entities.QuotaCycle{mainCycle}, now, 0, false); err != nil {
		t.Fatalf("initial recompute: %v", err)
	}
	var event entities.UsageEvent
	if err := db.Where("id = ?", evID).Take(&event).Error; err != nil {
		t.Fatalf("load event: %v", err)
	}
	hourlyRows, dailyRows, _, err := overview.BuildRows([]entities.UsageEvent{event})
	if err != nil {
		t.Fatalf("build overview rows: %v", err)
	}
	if err := repository.ApplyUsageOverviewAggregationPage(ctx, db, 0, evID, hourlyRows, dailyRows, now); err != nil {
		t.Fatalf("apply overview page: %v", err)
	}

	// 给超 90 天老周期人工置 cost_stale=true（模拟历史遗留中断）。
	if err := db.Model(&entities.QuotaCycle{}).Where("auth_index = ?", "auth-stale-old").Update("cost_stale", true).Error; err != nil {
		t.Fatalf("mark old cycle cost_stale: %v", err)
	}

	// 使用新定价（input 2.0/M, output 4.0/M）执行 ApplyUsageCostRecalculationBatch。
	mult := 1.0
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{
		Pricing: entities.ModelPriceSetting{
			Model:                "gpt-5",
			PricingStyle:         entities.ModelPricingStyleOpenAI,
			PromptPricePer1M:     2.0,
			CompletionPricePer1M: 4.0,
			PriceMultiplier:      &mult,
		},
	}})
	if err != nil {
		t.Fatalf("compile pricing snapshot: %v", err)
	}
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	scope := repository.UsageCostRecalculationScope{
		Start:      now.Add(-6 * time.Hour),
		End:        now,
		MaxEventID: evID,
	}
	pageEvents, _, _, err := repository.LoadUsageCostRecalculationPage(ctx, db, scope, 0)
	if err != nil {
		t.Fatalf("load recalculation page: %v", err)
	}
	if err := repository.ApplyUsageCostRecalculationBatch(ctx, db, scope, pageEvents, resolver); err != nil {
		t.Fatalf("apply recalculation batch: %v", err)
	}

	// 核对同事务内已将主周期标为 cost_stale=true。
	mainCycle = loadQuotaCycleByID(t, db, mainCycle.ID)
	if !mainCycle.CostStale {
		t.Fatalf("expected main cycle to be marked cost_stale=true after price change")
	}

	// 再插入 1 条 ID > MaxEventID 的新事件，验证 RecomputeCostStaleQuotaCycles 严格对齐 id <= MaxEventID。
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-price", now.Add(-2*time.Hour), false, 500_000, 9.99, true)

	recomputed, err := repository.RecomputeCostStaleQuotaCycles(ctx, db, now, scope.MaxEventID)
	if err != nil {
		t.Fatalf("recompute cost stale quota cycles: %v", err)
	}
	if recomputed != 2 {
		t.Fatalf("expected 2 cost_stale cycles handled, got %d", recomputed)
	}

	// 核对主周期：cost_stale 已清零，只包含 id <= MaxEventID 的那 1 条事件的新费用。
	var updatedEvent entities.UsageEvent
	if err := db.Where("id = ?", evID).Take(&updatedEvent).Error; err != nil {
		t.Fatalf("reload event: %v", err)
	}
	wantCost := *updatedEvent.CostUSD
	mainCycle = loadQuotaCycleByID(t, db, mainCycle.ID)
	if mainCycle.CostStale || !mainCycle.UsageReady || mainCycle.Requests != 1 || math.Abs(mainCycle.CostUSD-wantCost) > 1e-9 {
		t.Fatalf("unexpected main cycle after cost stale recompute: %+v wantCost=%v", mainCycle, wantCost)
	}
	var dropSeg entities.QuotaPercentSegment
	if err := db.Where("cycle_id = ? AND remaining_percent = ?", mainCycle.ID, 85).Take(&dropSeg).Error; err != nil {
		t.Fatalf("load drop segment: %v", err)
	}
	if !dropSeg.TransitionUsageReady || dropSeg.TransitionRequests != 1 || math.Abs(dropSeg.TransitionCostUSD-wantCost) > 1e-9 {
		t.Fatalf("unexpected drop segment after cost stale recompute: %+v wantCost=%v", dropSeg, wantCost)
	}

	// 核对超 90 天老周期：cost_stale 已清零且保持 usage_ready=false。
	var oldCycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-stale-old").Take(&oldCycle).Error; err != nil {
		t.Fatalf("load old cycle: %v", err)
	}
	if oldCycle.CostStale || oldCycle.UsageReady {
		t.Fatalf("expected old stale cycle to clear cost_stale and keep usage_ready=false: %+v", oldCycle)
	}
}

func TestRecomputeCostStaleQuotaCyclesRetriesBoundaryConflictAndFailsWhenExhausted(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	seedStaleCycle := func(t *testing.T, db *gorm.DB, authIndex string) (entities.QuotaCycle, int64) {
		t.Helper()
		ctx := context.Background()
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
			Provider:         "codex",
			AuthIndex:        authIndex,
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          now.Add(2 * time.Hour),
			RemainingPercent: 90,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		}}); err != nil {
			t.Fatalf("write observation: %v", err)
		}
		evID := insertQuotaTestEvent(t, db, "codex", "oauth", authIndex, now.Add(-time.Hour), false, 2000, 1.25, true)
		var cycle entities.QuotaCycle
		if err := db.Where("auth_index = ?", authIndex).Take(&cycle).Error; err != nil {
			t.Fatalf("load cycle: %v", err)
		}
		if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Update("cost_stale", true).Error; err != nil {
			t.Fatalf("mark cost_stale: %v", err)
		}
		return loadQuotaCycleByID(t, db, cycle.ID), evID
	}

	t.Run("recovers within bounded retries after concurrent boundary_version bumps", func(t *testing.T) {
		db := openQuotaAggregationTestDB(t)
		cycle, evID := seedStaleCycle(t, db, "auth-retry-ok")

		var attempts atomic.Int64
		const callbackName = "test:recompute_cost_stale_retry_ok"
		if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table != "quota_percent_segments" {
				return
			}
			// 前 2 次计算期间模拟并发观测推进 boundary_version，第 3 次允许正常落库。
			if attempts.Add(1) <= 2 {
				if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write).
					Model(&entities.QuotaCycle{}).
					Where("id = ?", cycle.ID).
					Update("boundary_version", gorm.Expr("boundary_version + 1")).Error; err != nil {
					t.Errorf("bump boundary_version: %v", err)
				}
			}
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

		recomputed, err := repository.RecomputeCostStaleQuotaCycles(context.Background(), db, now, evID)
		if err != nil {
			t.Fatalf("expected bounded retry to succeed, got error: %v", err)
		}
		if recomputed != 1 || attempts.Load() != 3 {
			t.Fatalf("expected recomputed=1 and attempts=3, got recomputed=%d attempts=%d", recomputed, attempts.Load())
		}
		updated := loadQuotaCycleByID(t, db, cycle.ID)
		if updated.CostStale || !updated.UsageReady || updated.Requests != 1 || math.Abs(updated.CostUSD-1.25) > 1e-9 {
			t.Fatalf("unexpected cycle state after retry recovery: %+v", updated)
		}
	})

	t.Run("returns explicit error when boundary_version conflict persists past 3 retries", func(t *testing.T) {
		db := openQuotaAggregationTestDB(t)
		cycle, evID := seedStaleCycle(t, db, "auth-retry-exhaust")

		var attempts atomic.Int64
		const callbackName = "test:recompute_cost_stale_retry_exhaust"
		if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table != "quota_percent_segments" {
				return
			}
			attempts.Add(1)
			if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write).
				Model(&entities.QuotaCycle{}).
				Where("id = ?", cycle.ID).
				Update("boundary_version", gorm.Expr("boundary_version + 1")).Error; err != nil {
				t.Errorf("bump boundary_version: %v", err)
			}
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

		recomputed, err := repository.RecomputeCostStaleQuotaCycles(context.Background(), db, now, evID)
		if err == nil || !strings.Contains(err.Error(), "boundary_version conflict") {
			t.Fatalf("expected explicit boundary_version conflict error, got recomputed=%d err=%v", recomputed, err)
		}
		if attempts.Load() != 4 {
			t.Fatalf("expected 1 initial attempt + 3 retries (4 total), got %d", attempts.Load())
		}
		remaining := loadQuotaCycleByID(t, db, cycle.ID)
		if !remaining.CostStale {
			t.Fatalf("expected cycle to remain cost_stale=true after conflict failure, got %+v", remaining)
		}
	})

	t.Run("treats concurrently deleted cycle as completed without error", func(t *testing.T) {
		db := openQuotaAggregationTestDB(t)
		cycle, evID := seedStaleCycle(t, db, "auth-retry-deleted")

		var deleted atomic.Bool
		const callbackName = "test:recompute_cost_stale_deleted"
		if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table != "quota_percent_segments" || !deleted.CompareAndSwap(false, true) {
				return
			}
			if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write).
				Where("cycle_id = ?", cycle.ID).Delete(&entities.QuotaPercentSegment{}).Error; err != nil {
				t.Errorf("delete segments: %v", err)
			}
			if err := db.Session(&gorm.Session{NewDB: true}).Clauses(dbresolver.Write).
				Where("id = ?", cycle.ID).Delete(&entities.QuotaCycle{}).Error; err != nil {
				t.Errorf("delete cycle: %v", err)
			}
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

		recomputed, err := repository.RecomputeCostStaleQuotaCycles(context.Background(), db, now, evID)
		if err != nil {
			t.Fatalf("expected concurrently deleted cycle not to fail recalculation, got %v", err)
		}
		if recomputed != 0 {
			t.Fatalf("expected recomputed=0 for deleted cycle, got %d", recomputed)
		}
	})
}
