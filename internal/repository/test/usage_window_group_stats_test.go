package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageWindowStatsCalculatorGroupsAggregatedRowsByRealModel(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local)
	end := start.Add(5 * time.Hour)
	alias := "gemini-user-alias"
	if err := db.Create(&[]entities.UsageEvent{
		{EventKey: "gemini", AuthIndex: "antigravity-auth", Model: "gemini-3-flash", Timestamp: start.Add(time.Hour), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{EventKey: "claude", AuthIndex: "antigravity-auth", Model: "claude-sonnet-4-6", ModelAlias: &alias, Timestamp: start.Add(2 * time.Hour), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{EventKey: "gpt", AuthIndex: "antigravity-auth", Model: "gpt-oss-120b-medium", Timestamp: start.Add(3 * time.Hour), InputTokens: 500_000, TotalTokens: 500_000},
	}).Error; err != nil {
		t.Fatalf("seed grouped usage events: %v", err)
	}

	resolver := usageWindowGroupPricingResolver(t)
	calculator, err := repository.NewUsageWindowStatsCalculator(context.Background(), db, resolver)
	if err != nil {
		t.Fatalf("NewUsageWindowStatsCalculator: %v", err)
	}
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

func TestUsageWindowStatsCalculatorMarksUnknownGroupsAndMissingPricesIncomplete(t *testing.T) {
	t.Run("unknown model group", func(t *testing.T) {
		db := openTestDatabase(t)
		start := time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local)
		end := start.Add(5 * time.Hour)
		if err := db.Create(&entities.UsageEvent{
			EventKey: "future", AuthIndex: "antigravity-auth", Model: "future-model", Timestamp: start.Add(time.Hour), InputTokens: 10, TotalTokens: 10,
		}).Error; err != nil {
			t.Fatalf("seed unknown grouped usage event: %v", err)
		}
		calculator, err := repository.NewUsageWindowStatsCalculator(context.Background(), db, usageWindowGroupPricingResolver(t))
		if err != nil {
			t.Fatalf("NewUsageWindowStatsCalculator: %v", err)
		}
		result, err := calculator.SumGroupsByAuthIndex(context.Background(), "antigravity-auth", start, &end, antigravityUsageWindowTestGroup)
		if err != nil {
			t.Fatalf("SumGroupsByAuthIndex: %v", err)
		}
		if result.Complete {
			t.Fatalf("expected an unknown positive-token model to make grouped attribution incomplete, got %+v", result)
		}
	})

	t.Run("missing group price", func(t *testing.T) {
		db := openTestDatabase(t)
		start := time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local)
		end := start.Add(5 * time.Hour)
		if err := db.Create(&entities.UsageEvent{
			EventKey: "unpriced", AuthIndex: "antigravity-auth", Model: "gpt-unpriced", Timestamp: start.Add(time.Hour), InputTokens: 10, TotalTokens: 10,
		}).Error; err != nil {
			t.Fatalf("seed unpriced grouped usage event: %v", err)
		}
		calculator, err := repository.NewUsageWindowStatsCalculator(context.Background(), db, pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver())
		if err != nil {
			t.Fatalf("NewUsageWindowStatsCalculator: %v", err)
		}
		result, err := calculator.SumGroupsByAuthIndex(context.Background(), "antigravity-auth", start, &end, antigravityUsageWindowTestGroup)
		if err != nil {
			t.Fatalf("SumGroupsByAuthIndex: %v", err)
		}
		stats := result.Groups["claude-gpt"]
		if stats.Tokens != 10 || stats.CostAvailable {
			t.Fatalf("expected tokens to remain known while group cost is unavailable, got %+v", stats)
		}
	})
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

func usageWindowGroupPricingResolver(t *testing.T) pricing.Resolver {
	t.Helper()
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{
		{Pricing: entities.ModelPriceSetting{Model: "gemini-3-flash", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1}},
		{Pricing: entities.ModelPriceSetting{Model: "claude-sonnet-4-6", PricingStyle: entities.ModelPricingStyleClaude, PromptPricePer1M: 2}},
		{Pricing: entities.ModelPriceSetting{Model: "gpt-oss-120b-medium", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 2}},
	})
	if err != nil {
		t.Fatalf("CompileSnapshot: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}
