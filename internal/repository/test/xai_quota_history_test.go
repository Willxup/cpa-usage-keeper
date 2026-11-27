package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestXAIQuotaHistoryDirtyMarkingAndWholeAccountAggregation(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	resetAt := now.Add(5 * 24 * time.Hour)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{
		{Provider: "xai", AuthIndex: "xai-repo", WindowRole: "weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: resetAt, RemainingPercent: 90, FirstObservedAt: now.Add(-2 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), ObservationCount: 1},
		{Provider: "xai", AuthIndex: "xai-repo", WindowRole: "weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: resetAt, RemainingPercent: 80, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
	}); err != nil {
		t.Fatalf("write xAI observations: %v", err)
	}
	clearQuotaCyclesDirty(t, db)

	insertModelScopedEvent(t, db, "xai", "xai-repo", "grok-4", now.Add(-90*time.Minute), 2000, 0.40, true)
	insertModelScopedEvent(t, db, "xai", "xai-repo", "grok-code-fast-1", now.Add(-80*time.Minute), 1000, 0.15, true)
	// 非 oauth 事件不得进入统计或标脏。
	insertQuotaTestEvent(t, db, "xai", "api_key", "xai-repo", now.Add(-70*time.Minute), false, 9000, 5.00, true)

	dirtyRes, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100)
	if err != nil {
		t.Fatalf("advance xAI dirty cursor: %v", err)
	}
	if dirtyRes.DirtyCyclesMarked != 1 {
		t.Fatalf("expected 1 xAI cycle marked dirty, got %+v", dirtyRes)
	}

	var cycle entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ?", "xai", "xai-repo").Take(&cycle).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, []entities.QuotaCycle{cycle}, now, 0, false); err != nil {
		t.Fatalf("recompute xAI cycle: %v", err)
	}
	cycle = loadQuotaCycleByID(t, db, cycle.ID)
	if !cycle.UsageReady || cycle.Dirty || cycle.Requests != 2 || cycle.TotalTokens != 3000 || math.Abs(cycle.CostUSD-0.55) > 1e-9 {
		t.Fatalf("unexpected aggregated xAI cycle: %+v", cycle)
	}
}
