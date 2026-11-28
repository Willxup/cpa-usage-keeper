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

func TestAntigravityQuotaHistoryPoolIsolationAndTargetedDirtyMarking(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	fiveReset := now.Add(2 * time.Hour)
	weeklyReset := now.Add(5 * 24 * time.Hour)
	// 为同一 Antigravity 账号写入四窗口周期。
	observations := []repositorydto.CodexMainQuotaObservation{
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "gemini_5h", WindowSeconds: 5 * 3600, ResetAtSource: "absolute", ResetAt: fiveReset, RemainingPercent: 100, FirstObservedAt: now.Add(-2 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "gemini_5h", WindowSeconds: 5 * 3600, ResetAtSource: "absolute", ResetAt: fiveReset, RemainingPercent: 95, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "gemini_weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: weeklyReset, RemainingPercent: 90, FirstObservedAt: now.Add(-2 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "claude_gpt_5h", WindowSeconds: 5 * 3600, ResetAtSource: "absolute", ResetAt: fiveReset, RemainingPercent: 100, FirstObservedAt: now.Add(-2 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "claude_gpt_5h", WindowSeconds: 5 * 3600, ResetAtSource: "absolute", ResetAt: fiveReset, RemainingPercent: 85, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-iso", WindowRole: "claude_gpt_weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: weeklyReset, RemainingPercent: 80, FirstObservedAt: now.Add(-2 * time.Hour), LastObservedAt: now.Add(-2 * time.Hour), ObservationCount: 1},
	}
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, observations); err != nil {
		t.Fatalf("write Antigravity observations: %v", err)
	}

	// 混合写入 Gemini（含 carbon/argon）、Claude、GPT 与未知模型事件，包含完整 6 类 Token。
	seedEvent := func(key, model string, ts time.Time, in, out, reason, cacheRead, cacheCreate int64, cost float64) {
		t.Helper()
		available := true
		ev := entities.UsageEvent{
			EventKey:            key,
			APIGroupKey:         "key-ag",
			Provider:            "antigravity",
			AuthType:            "oauth",
			AuthIndex:           "ag-iso",
			Model:               model,
			Timestamp:           ts,
			InputTokens:         in,
			OutputTokens:        out,
			ReasoningTokens:     reason,
			CacheReadTokens:     cacheRead,
			CacheCreationTokens: cacheCreate,
			TotalTokens:         in + out + reason + cacheRead + cacheCreate,
			CostUSD:             &cost,
			CostAvailable:       &available,
			CreatedAt:           ts,
		}
		if err := db.Create(&ev).Error; err != nil {
			t.Fatalf("insert Antigravity event %s: %v", key, err)
		}
	}
	ts := now.Add(-90 * time.Minute)
	seedEvent("ag-g1", "gemini-4-carbon-high", ts, 100, 50, 20, 10, 5, 0.30)
	seedEvent("ag-g2", "gemini-4-argon-high", ts.Add(time.Minute), 200, 100, 30, 15, 10, 0.70)
	seedEvent("ag-c1", "claude-sonnet-5-5-high", ts.Add(2*time.Minute), 400, 200, 40, 20, 15, 1.20)
	seedEvent("ag-c2", "gpt-oss-120b-medium", ts.Add(3*time.Minute), 300, 150, 10, 5, 5, 0.80)
	seedEvent("ag-unk", "future-model-x", ts.Add(4*time.Minute), 999, 999, 0, 0, 0, 9.99)

	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ?", "antigravity", "ag-iso").Order("id ASC").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, cycles, now, 0, false); err != nil {
		t.Fatalf("recompute Antigravity cycles: %v", err)
	}

	// 验证 Gemini 池与 Claude & GPT 池的请求数、6 类 Token 与费用严格隔离。
	byKey := make(map[string]entities.QuotaCycle, len(cycles))
	for _, cycle := range cycles {
		byKey[cycle.QuotaKey] = loadQuotaCycleByID(t, db, cycle.ID)
	}
	for _, key := range []string{"bucket.antigravity-gemini-models.5h", "bucket.antigravity-gemini-models.weekly"} {
		c := byKey[key]
		if !c.UsageReady || c.Requests != 2 || c.InputTokens != 300 || c.OutputTokens != 150 || c.ReasoningTokens != 50 ||
			c.CacheReadTokens != 25 || c.CacheCreationTokens != 15 || c.TotalTokens != 540 || math.Abs(c.CostUSD-1.00) > 1e-9 {
			t.Fatalf("Gemini pool %s misattributed: %+v", key, c)
		}
	}
	for _, key := range []string{"bucket.antigravity-claude-and-gpt-models.5h", "bucket.antigravity-claude-and-gpt-models.weekly"} {
		c := byKey[key]
		if !c.UsageReady || c.Requests != 2 || c.InputTokens != 700 || c.OutputTokens != 350 || c.ReasoningTokens != 50 ||
			c.CacheReadTokens != 25 || c.CacheCreationTokens != 20 || c.TotalTokens != 1145 || math.Abs(c.CostUSD-2.00) > 1e-9 {
			t.Fatalf("Claude & GPT pool %s misattributed: %+v", key, c)
		}
	}

	// 先推进游标到现有事件末尾并清理 dirty，再只追加 1 条 claude-sonnet 事件，验证绝不标脏 Gemini 池。
	if _, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100); err != nil {
		t.Fatal(err)
	}
	clearQuotaCyclesDirty(t, db)
	seedEvent("ag-c3", "claude-sonnet-5-5-high", now.Add(-10*time.Minute), 100, 50, 0, 0, 0, 0.25)

	dirtyRes, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100)
	if err != nil {
		t.Fatalf("advance dirty cursor for claude-only event: %v", err)
	}
	if dirtyRes.DirtyCyclesMarked != 2 {
		t.Fatalf("expected only 2 Claude & GPT cycles marked dirty, got %+v", dirtyRes)
	}
	for _, key := range []string{"bucket.antigravity-gemini-models.5h", "bucket.antigravity-gemini-models.weekly"} {
		if loadQuotaCycleByID(t, db, byKey[key].ID).Dirty {
			t.Fatalf("Gemini cycle %s must stay clean when only Claude event arrived", key)
		}
	}
	for _, key := range []string{"bucket.antigravity-claude-and-gpt-models.5h", "bucket.antigravity-claude-and-gpt-models.weekly"} {
		if !loadQuotaCycleByID(t, db, byKey[key].ID).Dirty {
			t.Fatalf("Claude & GPT cycle %s must be marked dirty", key)
		}
	}
}
