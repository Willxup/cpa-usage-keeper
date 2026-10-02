package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
)

func TestUsageWindowStatsCalculatorGroupsAggregatedRowsByRealModel(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local)
	end := start.Add(5 * time.Hour)
	alias := "gemini-user-alias"
	if err := db.Create(&[]entities.UsageEvent{
		{EventKey: "gemini", AuthIndex: "antigravity-auth", Model: "gemini-3-flash", Timestamp: start.Add(time.Hour), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(1), CostAvailable: windowAvailablePtr(true)},
		{EventKey: "claude", AuthIndex: "antigravity-auth", Model: "claude-sonnet-4-6", ModelAlias: &alias, Timestamp: start.Add(2 * time.Hour), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: windowCostPtr(2), CostAvailable: windowAvailablePtr(true)},
		{EventKey: "gpt", AuthIndex: "antigravity-auth", Model: "gpt-oss-120b-medium", Timestamp: start.Add(3 * time.Hour), InputTokens: 500_000, TotalTokens: 500_000, CostUSD: windowCostPtr(1), CostAvailable: windowAvailablePtr(true)},
	}).Error; err != nil {
		t.Fatalf("seed grouped usage events: %v", err)
	}
	calculator := newUsageWindowCalculatorForTest(t, db)
	result, err := calculator.SumGroupsByAuthIndex(context.Background(), "antigravity-auth", start, &end, antigravityUsageWindowTestGroup)
	if err != nil {
		t.Fatalf("SumGroupsByAuthIndex: %v", err)
	}
	if !result.Complete {
		t.Fatalf("expected every real model to have a known group, got %+v", result)
	}
	gemini := result.Groups["gemini"]
	if gemini.Tokens != 1_000_000 || math.Abs(gemini.Cost-1) > 0.000000001 || !gemini.CostAvailable {
		t.Fatalf("unexpected Gemini group stats: %+v", gemini)
	}
	claudeGPT := result.Groups["claude-gpt"]
	if claudeGPT.Tokens != 1_500_000 || math.Abs(claudeGPT.Cost-3) > 0.000000001 || !claudeGPT.CostAvailable {
		t.Fatalf("expected real Claude model to ignore its Gemini-looking alias, got %+v", claudeGPT)
	}
}

// 未知模型只有全部 Token 字段为零才不影响归组完整性；已知组沿事件已存可用性。
func TestUsageWindowStatsCalculatorKeepsUnknownGroupsAndStoredAvailability(t *testing.T) {
	for _, tc := range []struct {
		name      string
		model     string
		total     int64
		input     int64
		available bool
		wantGroup string
		complete  bool
	}{
		{name: "unknown positive total", model: "future-model", total: 10},
		{name: "unknown zero total positive input", model: "future-model", input: 10},
		{name: "unknown zero token", model: "future-model", complete: true, available: true},
		{name: "known unavailable cost", model: "gpt-unpriced", total: 10, wantGroup: "claude-gpt", complete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			start := time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local)
			end := start.Add(5 * time.Hour)
			if err := db.Create(&entities.UsageEvent{
				EventKey: "group-case", AuthIndex: "antigravity-auth", Model: tc.model, Timestamp: start.Add(time.Hour),
				InputTokens: tc.input, TotalTokens: tc.total, CostUSD: windowCostPtr(0), CostAvailable: windowAvailablePtr(tc.available),
			}).Error; err != nil {
				t.Fatalf("seed grouped event: %v", err)
			}
			calculator := newUsageWindowCalculatorForTest(t, db)
			result, err := calculator.SumGroupsByAuthIndex(context.Background(), "antigravity-auth", start, &end, antigravityUsageWindowTestGroup)
			if err != nil {
				t.Fatalf("SumGroupsByAuthIndex: %v", err)
			}
			if result.Complete != tc.complete {
				t.Fatalf("complete=%v, want %v: %+v", result.Complete, tc.complete, result)
			}
			if tc.wantGroup == "" {
				if len(result.Groups) != 0 {
					t.Fatalf("unknown model entered a known group: %+v", result)
				}
			} else if stats := result.Groups[tc.wantGroup]; stats.Tokens != tc.total || stats.CostAvailable {
				t.Fatalf("expected known tokens with unavailable cost, got %+v", stats)
			}
		})
	}
}

func antigravityUsageWindowTestGroup(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(normalized, "gemini-"):
		return "gemini", true
	case strings.HasPrefix(normalized, "claude-"), strings.HasPrefix(normalized, "gpt-"):
		return "claude-gpt", true
	default:
		return "", false
	}
}
