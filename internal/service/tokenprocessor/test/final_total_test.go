package tokenprocessor_test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestKnownExecutorContractsCorrectWrongNonzeroTotal(t *testing.T) {
	// 三类强合同分别证明 Claude Input fold、Gemini Output fold 与 Responses inclusive，最终都能唯一得到 Input+Output。
	tests := []struct {
		name           string
		executor       string
		tokens         tokenprocessor.TokenValues
		expectedInput  int64
		expectedOutput int64
		expectedTotal  int64
	}{
		{name: "claude", executor: "ClaudeExecutor", tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 10, CacheCreationTokens: 5, TotalTokens: 120}, expectedInput: 115, expectedOutput: 20, expectedTotal: 135},
		{name: "gemini", executor: "GeminiExecutor", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: 999}, expectedInput: 11, expectedOutput: 10, expectedTotal: 21},
		{name: "responses", executor: "CodexExecutor", tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, CacheReadTokens: 30, CacheCreationTokens: 10, TotalTokens: 125}, expectedInput: 100, expectedOutput: 20, expectedTotal: 120},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, mustResolveExecutor(t, test.executor))
			if result.Tokens.InputTokens != test.expectedInput || result.Tokens.OutputTokens != test.expectedOutput || result.Tokens.TotalTokens != test.expectedTotal {
				t.Fatalf("expected input=%d output=%d total=%d, got %+v", test.expectedInput, test.expectedOutput, test.expectedTotal, result.Tokens)
			}
			if !hasAction(result, "correct_nonzero_total") || result.Outcome != tokenprocessor.TokenOutcomeCorrected {
				t.Fatalf("expected deterministic nonzero correction, got outcome=%q actions=%+v", result.Outcome, result.Actions)
			}
		})
	}
}

func TestClaudeIdentityCorrectsOnlyTheVerifiedLegacyMissingCacheFingerprint(t *testing.T) {
	// 附件历史指纹：旧 Input/Total 同时漏掉全部 cache，能唯一恢复为 22,599 / 22,852。
	verified := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         3085,
		OutputTokens:        253,
		CacheReadTokens:     0,
		CacheCreationTokens: 19514,
		TotalTokens:         3338,
	}, mustResolveIdentity(t, "", "claude"))
	if verified.Tokens.InputTokens != 22599 || verified.Tokens.TotalTokens != 22852 || !hasAction(verified, "correct_nonzero_total") {
		t.Fatalf("expected verified Claude history correction, got tokens=%+v actions=%+v", verified.Tokens, verified.Actions)
	}

	// 非零 Total 不符合精确旧指纹时只能保留，不能因为 identity 名叫 Claude 就扩大强纠错。
	ambiguous := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CacheCreationTokens: 10, TotalTokens: 125}, mustResolveIdentity(t, "", "claude"))
	if ambiguous.Tokens.InputTokens != 110 || ambiguous.Tokens.TotalTokens != 125 || hasAction(ambiguous, "correct_nonzero_total") {
		t.Fatalf("expected non-fingerprint Claude identity event to preserve Total, got tokens=%+v actions=%+v", ambiguous.Tokens, ambiguous.Actions)
	}
}

func TestIdentityAndStrictHandlersDoNotGainExecutorCorrectionAuthority(t *testing.T) {
	// codex identity 知道历史格式但没有本次 parser 合同，因此只报告不一致；Kimi/unknown 更不能猜测。
	tests := []struct {
		name       string
		resolution tokenprocessor.HandlerResolution
		tokens     tokenprocessor.TokenValues
	}{
		{name: "codex identity", resolution: mustResolveIdentity(t, "", "codex"), tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 125}},
		{name: "kimi executor", resolution: mustResolveExecutor(t, "KimiExecutor"), tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 125}},
		{name: "unknown default", resolution: mustResolveIdentity(t, "FutureExecutor", "custom"), tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, TotalTokens: 125}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, test.resolution)
			if result.Tokens.TotalTokens != 125 || hasAction(result, "correct_nonzero_total") {
				t.Fatalf("expected nonzero Total to remain untouched, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
			}
		})
	}
}

func TestCanonicalCorrectionRequiresParentChildInvariants(t *testing.T) {
	// Responses Input 已含 cache、Output 已含 reasoning；子项大于父项时只能指出冲突，不能抬高父项或重算 Total。
	tests := []struct {
		name          string
		tokens        tokenprocessor.TokenValues
		violationCode string
	}{
		{name: "cache exceeds input", tokens: tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 8, CacheCreationTokens: 5, TotalTokens: 999}, violationCode: "input_below_cache_components"},
		{name: "reasoning exceeds output", tokens: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 3, ReasoningTokens: 5, TotalTokens: 999}, violationCode: "output_below_reasoning"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, mustResolveExecutor(t, "CodexExecutor"))
			if result.Tokens.TotalTokens != 999 || hasAction(result, "correct_nonzero_total") || !hasViolation(result, test.violationCode) {
				t.Fatalf("expected invariant violation without Total correction, got tokens=%+v actions=%+v violations=%+v", result.Tokens, result.Actions, result.Violations)
			}
			if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
				t.Fatalf("expected ambiguous outcome, got %q", result.Outcome)
			}
		})
	}
}

func TestNegativeClampAndOverflowRemoveStrongTotalAuthority(t *testing.T) {
	// clamped Input 参与 canonical Total，不能用清零后的值纠正非零 Total。
	clamped := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: -5, OutputTokens: 7, TotalTokens: 99}, mustResolveExecutor(t, "CodexExecutor"))
	if clamped.Tokens.InputTokens != 0 || clamped.Tokens.TotalTokens != 99 || !hasAction(clamped, "clamp_negative_token") || hasAction(clamped, "correct_nonzero_total") {
		t.Fatalf("expected clamp with preserved Total, got tokens=%+v actions=%+v", clamped.Tokens, clamped.Actions)
	}
	if clamped.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected clamped dependency to remain ambiguous, got %q", clamped.Outcome)
	}

	// Input+Output 超出 int64 时不能回绕，也不能阻塞 Process 返回其它字段。
	overflow := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: math.MaxInt64, OutputTokens: 1, TotalTokens: 42}, mustResolveExecutor(t, "CodexExecutor"))
	if overflow.Tokens.TotalTokens != 42 || !hasViolation(overflow, "token_arithmetic_overflow") || overflow.Outcome != tokenprocessor.TokenOutcomeOverflow {
		t.Fatalf("expected overflow to preserve Total and report outcome, got tokens=%+v violations=%+v outcome=%q", overflow.Tokens, overflow.Violations, overflow.Outcome)
	}
}

func TestZeroTotalAndLegalAllZeroKeepExistingCompatibility(t *testing.T) {
	// canonical executor 的零 Total 仍是既有兼容回填，不应被标记为新增 corrected。
	backfilled := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20}, mustResolveExecutor(t, "CodexExecutor"))
	if backfilled.Tokens.TotalTokens != 120 || !hasAction(backfilled, "backfill_zero_total") || backfilled.Outcome != tokenprocessor.TokenOutcomeCompatibility {
		t.Fatalf("expected zero Total compatibility, got tokens=%+v actions=%+v outcome=%q", backfilled.Tokens, backfilled.Actions, backfilled.Outcome)
	}

	// EnsurePublished、失败事件和 tier-only 事件都可能全零；没有违反任何合同就必须保持 valid。
	zero := tokenprocessor.Process(tokenprocessor.TokenValues{}, mustResolveExecutor(t, "CodexExecutor"))
	if zero.Tokens != (tokenprocessor.TokenValues{}) || len(zero.Actions) != 0 || len(zero.Violations) != 0 || zero.Outcome != tokenprocessor.TokenOutcomeValid {
		t.Fatalf("expected legal all-zero event to stay valid, got %+v", zero)
	}
}
