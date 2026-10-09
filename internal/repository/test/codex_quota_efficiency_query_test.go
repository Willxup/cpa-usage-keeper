package test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestCodexQuotaEfficiencyPricingProjectionFollowsSnapshot(t *testing.T) {
	for _, field := range []string{"api_group_key", "service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type", "model_alias"} {
		t.Run(field, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
			seedCodexQuotaEfficiencyCycle(t, db, "codex-auth", now.Add(-5*time.Hour), now.Add(time.Hour), []codexQuotaEfficiencySegmentSeed{
				{remaining: 90, first: now.Add(-3 * time.Hour), last: now.Add(-3 * time.Hour)},
				{remaining: 89, first: now.Add(-time.Hour), last: now.Add(-time.Hour)},
			})
			seedCodexQuotaEfficiencyUsage(t, db,
				usageEventForQuotaEfficiency("normal", "oauth", "codex-auth", now.Add(-2*time.Hour), 1_000_000),
				usageEventForQuotaEfficiency("special", "oauth", "codex-auth", now.Add(-90*time.Minute), 1_000_000),
			)
			if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", "special").Update(field, "special").Error; err != nil {
				t.Fatal(err)
			}
			// 同一份历史分别用无规则及有规则快照查询；字段只有实际参与规则时才影响金额。
			for _, active := range []bool{false, true} {
				var rules []pricing.RuleConfig
				wantCost := 2.0
				if active {
					rules = []pricing.RuleConfig{{Key: field, Value: "special", Multiplier: 2}}
					wantCost = 3
				}
				result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{
					AuthIndex: "codex-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour),
				}, codexQuotaEfficiencyPricingResolverWithRules(t, rules))
				if err != nil {
					t.Fatal(err)
				}
				assertCodexQuotaEfficiencyUsage(t, result.Cycles[0].Usage, 2_000_000, wantCost, true)
				assertCodexQuotaEfficiencyUsage(t, result.Cycles[0].Transitions[0].Usage, 2_000_000, wantCost, true)
			}
		})
	}
}

func TestCodexQuotaEfficiencyPreservesLegacyPricingGroups(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		mutate   func(*entities.UsageEvent)
		wantCost float64
	}{
		{name: "cache exceeds input", mutate: func(e *entities.UsageEvent) { e.CacheReadTokens = 2_000_000 }, wantCost: 1},
		{name: "cache creation exceeds input", mutate: func(e *entities.UsageEvent) { e.CacheCreationTokens = 2_000_000 }, wantCost: 1},
		{name: "negative input", mutate: func(e *entities.UsageEvent) { e.InputTokens = -1_000_000 }, wantCost: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
			seedCodexQuotaEfficiencyCycle(t, db, "codex-auth", now.Add(-10*time.Hour), now.Add(-5*time.Hour), []codexQuotaEfficiencySegmentSeed{
				{remaining: 80, first: now.Add(-9 * time.Hour), last: now.Add(-9 * time.Hour)},
				{remaining: 79, first: now.Add(-8 * time.Hour), last: now.Add(-8 * time.Hour)},
			})
			seedCodexQuotaEfficiencyCycle(t, db, "codex-auth", now.Add(-5*time.Hour), now.Add(time.Hour), []codexQuotaEfficiencySegmentSeed{
				{remaining: 90, first: now.Add(-3 * time.Hour), last: now.Add(-3 * time.Hour)},
				{remaining: 89, first: now.Add(-time.Hour), last: now.Add(-time.Hour)},
			})
			normal := usageEventForQuotaEfficiency("normal", "oauth", "codex-auth", now.Add(-2*time.Hour), 1_000_000)
			legacy := usageEventForQuotaEfficiency("legacy", "oauth", "codex-auth", now.Add(-90*time.Minute), 1_000_000)
			legacy.ReasoningEffort = "high"
			testCase.mutate(&legacy)
			seedCodexQuotaEfficiencyUsage(t, db,
				usageEventForQuotaEfficiency("completed", "oauth", "codex-auth", now.Add(-8*time.Hour-30*time.Minute), 400_000),
				normal, legacy,
			)
			result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{
				AuthIndex: "codex-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour),
			}, codexQuotaEfficiencyPricingResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			assertCodexQuotaEfficiencyUsage(t, result.Cycles[0].Usage, 2_000_000, testCase.wantCost, true)
			assertCodexQuotaEfficiencyUsage(t, result.Cycles[0].Transitions[0].Usage, 2_000_000, testCase.wantCost, true)
			// 旧数据位于后一个周期时，已经处理过的周期及变化区间也不能重复累加。
			assertCodexQuotaEfficiencyUsage(t, result.Cycles[1].Usage, 400_000, 0.4, true)
			assertCodexQuotaEfficiencyUsage(t, result.Cycles[1].Transitions[0].Usage, 400_000, 0.4, true)
			if result.Cycles[0].Usage.Requests != 2 || result.Cycles[0].Transitions[0].Usage.Requests != 2 {
				t.Fatalf("unexpected request totals: %+v", result.Cycles[0])
			}
		})
	}
}

func TestCodexQuotaEfficiencyReadsNullableLegacyFieldsAndAlias(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	seedCodexQuotaEfficiencyCycle(t, db, "codex-auth", now.Add(-5*time.Hour), now.Add(time.Hour), []codexQuotaEfficiencySegmentSeed{
		{remaining: 90, first: now.Add(-3 * time.Hour), last: now.Add(-3 * time.Hour)},
		{remaining: 89, first: now.Add(-time.Hour), last: now.Add(-time.Hour)},
	})
	event := usageEventForQuotaEfficiency("legacy-null", "oauth", "codex-auth", now.Add(-time.Hour), 0)
	seedCodexQuotaEfficiencyUsage(t, db, event)
	if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", event.EventKey).Updates(map[string]any{
		"api_group_key": nil, "endpoint": nil, "model": nil, "failed": nil, "input_tokens": nil,
		"output_tokens": nil, "reasoning_tokens": nil, "total_tokens": nil,
	}).Error; err != nil {
		t.Fatal(err)
	}
	alias := "priced-model"
	aliased := usageEventForQuotaEfficiency("alias", "oauth", "codex-auth", now.Add(-time.Hour), 1_000_000)
	aliased.Model, aliased.ModelAlias, aliased.Failed = "unknown-model", &alias, true
	seedCodexQuotaEfficiencyUsage(t, db, aliased)
	result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{
		AuthIndex: "codex-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour),
	}, codexQuotaEfficiencyPricingResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, usage := range []repositorydto.CodexQuotaEfficiencyUsage{result.Cycles[0].Usage, result.Cycles[0].Transitions[0].Usage} {
		assertCodexQuotaEfficiencyUsage(t, usage, 1_000_000, 1, true)
		if usage.Requests != 2 || usage.SuccessfulRequests != 1 || usage.FailedRequests != 1 {
			t.Fatalf("unexpected nullable/boundary counters: %+v", usage)
		}
	}
}

func TestCodexQuotaEfficiencyPreservesTokenComponents(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	seedCodexQuotaEfficiencyCycle(t, db, "codex-auth", now.Add(-5*time.Hour), now.Add(time.Hour), []codexQuotaEfficiencySegmentSeed{
		{remaining: 90, first: now.Add(-3 * time.Hour), last: now.Add(-3 * time.Hour)},
		{remaining: 89, first: now.Add(-time.Hour), last: now.Add(-time.Hour)},
	})
	first := usageEventForQuotaEfficiency("first", "oauth", "codex-auth", now.Add(-2*time.Hour), 1_000_000)
	first.OutputTokens, first.ReasoningTokens = 200_000, 70_000
	first.CacheReadTokens, first.CacheCreationTokens, first.TotalTokens = 100_000, 50_000, 1_200_000
	second := usageEventForQuotaEfficiency("second", "oauth", "codex-auth", now.Add(-90*time.Minute), 2_000_000)
	second.OutputTokens, second.ReasoningTokens = 300_000, 80_000
	second.CacheReadTokens, second.CacheCreationTokens, second.TotalTokens = 200_000, 25_000, 2_300_000
	second.Endpoint, second.Failed = "/different-endpoint", true
	seedCodexQuotaEfficiencyUsage(t, db, first, second)
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: entities.ModelPriceSetting{
		Model: "priced-model", PromptPricePer1M: 2, CompletionPricePer1M: 4, CacheReadPricePer1M: 0.5, CacheWritePricePer1M: 1,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{
		AuthIndex: "codex-auth", Now: now, RangeStart: now.Add(-30 * 24 * time.Hour),
	}, pricing.NewCatalog(snapshot).NewResolver())
	if err != nil {
		t.Fatal(err)
	}
	for _, usage := range []repositorydto.CodexQuotaEfficiencyUsage{result.Cycles[0].Usage, result.Cycles[0].Transitions[0].Usage} {
		assertCodexQuotaEfficiencyUsage(t, usage, 3_500_000, 7.475, true)
		if usage.InputTokens != 3_000_000 || usage.OutputTokens != 500_000 || usage.ReasoningTokens != 150_000 ||
			usage.CacheReadTokens != 300_000 || usage.CacheCreationTokens != 75_000 ||
			usage.Requests != 2 || usage.SuccessfulRequests != 1 || usage.FailedRequests != 1 {
			t.Fatalf("unexpected token components or counters: %+v", usage)
		}
	}
}

func BenchmarkCodexQuotaEfficiencyHistory(b *testing.B) {
	for _, testCase := range []struct {
		name   string
		events int
		rules  []pricing.RuleConfig
	}{
		{name: "30k_no_rules", events: 30_000},
		{name: "100k_no_rules", events: 100_000},
		{name: "30k_with_rules", events: 30_000, rules: []pricing.RuleConfig{
			{Key: "service_tier", Value: "priority", Multiplier: 2},
			{Key: "reasoning_effort", Value: "high", Multiplier: 1.5},
		}},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), "quota.db")})
			if err != nil {
				b.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = sqlDB.Close() })
			now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
			start := now.Add(-30 * 24 * time.Hour)
			for cycleIndex := 0; cycleIndex < 144; cycleIndex++ {
				cycleStart := start.Add(time.Duration(cycleIndex) * 5 * time.Hour)
				segments := make([]codexQuotaEfficiencySegmentSeed, 101)
				for point := range segments {
					observed := cycleStart.Add(time.Duration(point) * 3 * time.Minute)
					segments[point] = codexQuotaEfficiencySegmentSeed{remaining: 100 - point, first: observed, last: observed}
				}
				seedCodexQuotaEfficiencyCycle(b, db, "codex-auth", cycleStart, cycleStart.Add(5*time.Hour), segments)
			}
			for offset := 0; offset < testCase.events; offset += 100 {
				events := make([]entities.UsageEvent, 0, 100)
				for index := offset; index < min(offset+100, testCase.events); index++ {
					at := start.Add(time.Duration(index) * (30 * 24 * time.Hour / time.Duration(testCase.events)))
					event := usageEventForQuotaEfficiency(fmt.Sprintf("event-%d", index), "oauth", "codex-auth", at, 1000)
					event.APIGroupKey = fmt.Sprintf("group-%d", index%7)
					event.Endpoint = fmt.Sprintf("/endpoint-%d", index%3)
					event.ServiceTier = []string{"standard", "priority"}[index%2]
					event.ReasoningEffort = []string{"low", "medium", "high"}[index%3]
					events = append(events, event)
				}
				seedCodexQuotaEfficiencyUsage(b, db, events...)
			}
			resolver := codexQuotaEfficiencyPricingResolverWithRules(b, testCase.rules)
			query := repositorydto.CodexQuotaEfficiencyQuery{AuthIndex: "codex-auth", Now: now, RangeStart: start}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, query, resolver)
				if err != nil {
					b.Fatal(err)
				}
				var requests int64
				var cost float64
				for _, cycle := range result.Cycles {
					requests += cycle.Usage.Requests
					cost += cycle.Usage.TotalCostUSD
				}
				if len(result.Cycles) != 144 || requests != int64(testCase.events) || (len(testCase.rules) == 0 && math.Abs(cost-float64(testCase.events)/1000) > 1e-8) {
					b.Fatalf("unexpected history: cycles=%d requests=%d cost=%f", len(result.Cycles), requests, cost)
				}
			}
		})
	}
}
