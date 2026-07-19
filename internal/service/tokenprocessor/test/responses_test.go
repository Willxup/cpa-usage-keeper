package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestResponsesExecutorKeepsInclusiveInputAndOutput(t *testing.T) {
	// Codex/xAI Responses 的 Input 已含 cache、Output 已含 reasoning，任何重复相加都会造成双计数。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         100,
		OutputTokens:        20,
		ReasoningTokens:     5,
		CachedTokens:        30,
		CacheReadTokens:     30,
		CacheCreationTokens: 10,
		TotalTokens:         120,
	}, mustResolveExecutor(t, "CodexExecutor"))

	if result.Tokens.InputTokens != 100 || result.Tokens.OutputTokens != 20 || result.Tokens.ReasoningTokens != 5 || result.Tokens.TotalTokens != 120 {
		t.Fatalf("expected inclusive Responses values to stay unchanged, got %+v", result.Tokens)
	}
}

func TestResponsesZeroTotalDoesNotAddReasoningTwice(t *testing.T) {
	// Codex Output 已包含 reasoning；旧零 Total 只能补 Input+Output，不能再次把 reasoning 加入总量。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     11,
		OutputTokens:    10,
		ReasoningTokens: 3,
	}, mustResolveExecutor(t, "CodexExecutor"))

	if result.Tokens.OutputTokens != 10 || result.Tokens.TotalTokens != 21 {
		t.Fatalf("expected inclusive Responses zero Total to become 21 without reasoning duplication, got %+v", result.Tokens)
	}
	if !hasAction(result, tokenprocessor.ActionBackfillZeroTotal) {
		t.Fatalf("expected existing zero Total action, got %+v", result.Actions)
	}
}

func TestNonClaudeCacheAliasKeepsExistingOneWayFallback(t *testing.T) {
	// 非 Claude 继续只在显式 read 为零时从 cached 单向回填，不能反向覆盖 legacy cached。
	backfilled := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CachedTokens: 30, TotalTokens: 120}, mustResolveExecutor(t, "CodexExecutor"))
	if backfilled.Tokens.CacheReadTokens != 30 || backfilled.Tokens.CachedTokens != 30 || !hasAction(backfilled, "backfill_cache_read_alias") {
		t.Fatalf("expected cached to backfill read, got tokens=%+v actions=%+v", backfilled.Tokens, backfilled.Actions)
	}

	explicit := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CachedTokens: 30, CacheReadTokens: 18, TotalTokens: 120}, mustResolveExecutor(t, "CodexExecutor"))
	if explicit.Tokens.CacheReadTokens != 18 || explicit.Tokens.CachedTokens != 30 {
		t.Fatalf("expected explicit read to win without overwriting cached, got %+v", explicit.Tokens)
	}
}

func TestGeminiAndOpenAICompatibilityKeepNonClaudeCacheAlias(t *testing.T) {
	// Gemini 与 OpenAI Compatibility 各自调用共享 cache alias；两种证据来源都要保护旧 cached→read 规则。
	tests := []struct {
		name       string
		resolution tokenprocessor.HandlerResolution
	}{
		{name: "Gemini executor", resolution: mustResolveExecutor(t, "GeminiExecutor")},
		{name: "Gemini identity", resolution: mustResolveIdentity(t, "", "gemini")},
		{name: "OpenAI compatibility executor", resolution: mustResolveExecutor(t, "OpenAICompatExecutor")},
		{name: "OpenAI compatibility identity", resolution: mustResolveIdentity(t, "", "openai")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(tokenprocessor.TokenValues{
				InputTokens:         100,
				OutputTokens:        20,
				CachedTokens:        30,
				CacheCreationTokens: 10,
				TotalTokens:         120,
			}, test.resolution)
			if result.Tokens.CachedTokens != 30 || result.Tokens.CacheReadTokens != 30 || result.Tokens.CacheCreationTokens != 10 {
				t.Fatalf("expected cached tokens to backfill read while preserving creation, got %+v", result.Tokens)
			}
			if !hasAction(result, tokenprocessor.ActionBackfillCacheReadAlias) {
				t.Fatalf("expected cache read compatibility action, got %+v", result.Actions)
			}
		})
	}
}

func TestNonClaudeCacheAliasRejectsReadClampedToZero(t *testing.T) {
	// raw read=-1 被 clamp 成零不等于“显式 read 缺失”；cached 不能借这个伪零值覆盖损坏字段。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CachedTokens: 30, CacheReadTokens: -1, TotalTokens: 120}, mustResolveExecutor(t, "CodexExecutor"))
	if result.Tokens.CacheReadTokens != 0 || hasAction(result, "backfill_cache_read_alias") {
		t.Fatalf("expected cache alias to reject clamped read, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected damaged read to be ambiguous, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}
}

func TestCodexExecutorKeepsExistingCachedOnlyFallback(t *testing.T) {
	// CPA payload 没有 additional-model 来源标记；沿用生产旧规则，只从 cached 补 read，不能猜测 Input。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{CachedTokens: 30, TotalTokens: 30}, mustResolveExecutor(t, "CodexExecutor"))
	if result.Tokens.InputTokens != 0 || result.Tokens.CacheReadTokens != 30 || result.Tokens.TotalTokens != 30 {
		t.Fatalf("expected Codex cached-only event to keep legacy cache alias only, got %+v", result.Tokens)
	}
	if !hasAction(result, tokenprocessor.ActionBackfillCacheReadAlias) {
		t.Fatalf("cached-only event must retain only the existing cache-read alias, got %+v", result.Actions)
	}
	// 该形态是生产旧规则已接受的 Codex 兼容数据；保持值不代表新增异常，不应产生事件级告警。
	if result.Outcome != tokenprocessor.TokenOutcomeCompatibility || len(result.Violations) != 0 {
		t.Fatalf("expected legacy cached-only shape to remain quiet compatibility, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}

	// identity-only Codex 同样只保留普通 cached→read，不反推 Input。
	identity := tokenprocessor.Process(tokenprocessor.TokenValues{CachedTokens: 30, TotalTokens: 30}, mustResolveIdentity(t, "", "codex"))
	if identity.Tokens.InputTokens != 0 || identity.Tokens.CacheReadTokens != 30 {
		t.Fatalf("expected identity-only cached event to avoid strong restore, got tokens=%+v actions=%+v", identity.Tokens, identity.Actions)
	}
}

func TestCodexCreationOnlyShapeDoesNotGainAnUnprovenCompatibilityRule(t *testing.T) {
	// CPA 能解析 creation 字段不等于生产中存在 additional-model creation-only 合同；本轮只记录父子冲突，不反推 Input。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{CacheCreationTokens: 30, TotalTokens: 30}, mustResolveExecutor(t, "CodexExecutor"))
	if result.Tokens.InputTokens != 0 || result.Tokens.TotalTokens != 30 {
		t.Fatalf("expected creation-only shape to avoid dedicated restoration, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if !hasViolation(result, "input_below_cache_components") || result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected ordinary parent-child violation, got violations=%+v outcome=%q", result.Violations, result.Outcome)
	}
}
