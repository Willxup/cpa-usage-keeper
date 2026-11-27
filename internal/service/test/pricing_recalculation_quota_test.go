package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// TestPricingRecalculationRepairsLeftoverCostStaleQuotaCycleWhenTotalIsZero 验证即使本次重算范围 total==0，也会修复遗留的 cost_stale 周期。
func TestPricingRecalculationRepairsLeftoverCostStaleQuotaCycleWhenTotalIsZero(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db, _ := openResolverServiceTestPools(t)
	now := time.Date(2026, 11, 25, 12, 30, 0, 0, time.UTC)

	// 仅在更早时间插入 1 条事件（使热表非空），本次选择的最近 1 小时范围内 total == 0。
	cost, available := 0.75, true
	ev := entities.UsageEvent{
		EventKey:      "earlier-oauth-event",
		APIGroupKey:   "key-1",
		Provider:      "codex",
		AuthType:      "oauth",
		AuthIndex:     "auth-recover",
		Model:         "gpt-5",
		Timestamp:     now.Add(-3 * time.Hour),
		InputTokens:   1000,
		TotalTokens:   1000,
		CostUSD:       &cost,
		CostAvailable: &available,
	}
	if err := db.Create(&ev).Error; err != nil {
		t.Fatalf("insert earlier event: %v", err)
	}

	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-recover",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          now.Add(2 * time.Hour),
		RemainingPercent: 90,
		FirstObservedAt:  now.Add(-2 * time.Hour),
		LastObservedAt:   now.Add(-2 * time.Hour),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write quota observation: %v", err)
	}
	// 模拟上次改价中断遗留的 cost_stale=true 与旧金额。
	if err := db.Model(&entities.QuotaCycle{}).Where("auth_index = ?", "auth-recover").
		Updates(map[string]any{"cost_stale": true, "cost_usd": 0.01, "usage_ready": true}).Error; err != nil {
		t.Fatalf("mark cycle cost_stale: %v", err)
	}

	provider, _, _, _, stop := newPricingRecalculationTestProvider(t, db, now)
	defer stop()
	options, err := provider.GetPricingRecalculationOptions(context.Background())
	if err != nil || options.LatestStart == nil {
		t.Fatalf("get recalculation options: %+v err=%v", options, err)
	}
	started, err := provider.StartPricingRecalculation(context.Background(), servicedto.StartRecalculationRequest{
		StartAt:        *options.LatestStart,
		ConfigRevision: options.ConfigRevision,
	})
	if err != nil || !started.Started {
		t.Fatalf("start recalculation: %+v err=%v", started, err)
	}

	provider.WaitPricingRecalculation()
	task, err := provider.CurrentPricingRecalculation(context.Background())
	if err != nil || task == nil || task.Status != servicedto.RecalculationCompleted || task.TotalCount == nil || *task.TotalCount != 0 {
		t.Fatalf("expected completed zero-event recalculation, got %+v err=%v", task, err)
	}

	var cycle entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-recover").Take(&cycle).Error; err != nil {
		t.Fatalf("load recovered cycle: %v", err)
	}
	if cycle.CostStale || !cycle.UsageReady || cycle.Requests != 1 || math.Abs(cycle.CostUSD-0.75) > 1e-9 {
		t.Fatalf("expected leftover cost_stale cycle to be repaired even when total==0: %+v", cycle)
	}
}
