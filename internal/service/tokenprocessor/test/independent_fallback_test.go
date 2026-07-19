package tokenprocessor_test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestParentChildConflictsDoNotBlockExistingZeroTotalFallback(t *testing.T) {
	// 旧零 Total 规则只依赖可信的 Input 与 Output；父子字段冲突必须保留诊断，但不能阻断这条独立证据链。
	tests := []struct {
		name              string
		tokens            tokenprocessor.TokenValues
		expectedViolation string
	}{
		{
			name:              "cache components exceed input",
			tokens:            tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 20},
			expectedViolation: tokenprocessor.ViolationInputBelowCache,
		},
		{
			name:              "reasoning exceeds output",
			tokens:            tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 2, ReasoningTokens: 5},
			expectedViolation: tokenprocessor.ViolationOutputBelowReasoning,
		},
		{
			name:              "cache components overflow",
			tokens:            tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: math.MaxInt64, CacheCreationTokens: 1},
			expectedViolation: tokenprocessor.ViolationTokenArithmeticOverflow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, mustResolveExecutor(t, "CodexExecutor"))
			if result.Tokens.TotalTokens != test.tokens.InputTokens+test.tokens.OutputTokens || !hasAction(result, tokenprocessor.ActionBackfillZeroTotal) {
				t.Fatalf("expected independent zero Total fallback, got tokens=%+v actions=%+v violations=%+v", result.Tokens, result.Actions, result.Violations)
			}
			if !hasViolation(result, test.expectedViolation) {
				t.Fatalf("expected parent-child violation %q to remain visible, got %+v", test.expectedViolation, result.Violations)
			}
		})
	}
}

func TestUnrelatedClampedFieldsDoNotBlockExistingZeroTotalFallback(t *testing.T) {
	// 旧规则只依赖正常的 Input 与 Output；其它字段损坏时，只能停止依赖损坏字段的转换，不能连带停止零 Total 回填。
	tests := []struct {
		name            string
		tokens          tokenprocessor.TokenValues
		resolution      tokenprocessor.HandlerResolution
		forbiddenAction string
	}{
		{
			name:            "Claude cache read damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: -1},
			resolution:      mustResolveExecutor(t, "ClaudeExecutor"),
			forbiddenAction: "normalize_claude_input",
		},
		{
			name:            "Gemini executor reasoning damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, ReasoningTokens: -1},
			resolution:      mustResolveExecutor(t, "GeminiExecutor"),
			forbiddenAction: "normalize_gemini_output",
		},
		{
			name:            "Gemini identity reasoning damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, ReasoningTokens: -1},
			resolution:      mustResolveIdentity(t, "", "gemini"),
			forbiddenAction: "normalize_gemini_output",
		},
		{
			name:            "OpenAI compatibility reasoning damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, ReasoningTokens: -1},
			resolution:      mustResolveExecutor(t, "OpenAICompatExecutor"),
			forbiddenAction: "apply_issue272_reasoning_fold",
		},
		{
			name:            "Responses cache read damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: -1},
			resolution:      mustResolveExecutor(t, "CodexExecutor"),
			forbiddenAction: "backfill_cache_read_alias",
		},
		{
			name:            "Kimi cache read damage does not block parent fallback",
			tokens:          tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: -1},
			resolution:      mustResolveExecutor(t, "KimiExecutor"),
			forbiddenAction: "backfill_cache_read_alias",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, test.resolution)
			if result.Tokens.TotalTokens != 15 || !hasAction(result, "backfill_zero_total") {
				t.Fatalf("expected independent zero Total fallback to produce 15, got tokens=%+v actions=%+v violations=%+v", result.Tokens, result.Actions, result.Violations)
			}
			if hasAction(result, test.forbiddenAction) {
				t.Fatalf("damaged field must not trigger dependent action %q: %+v", test.forbiddenAction, result.Actions)
			}
		})
	}
}

func TestClampedContractFieldsDoNotRegainNonzeroTotalCorrection(t *testing.T) {
	// 恢复旧零值补法不能顺带放宽新纠错权限；只要强合同依赖字段损坏，错误的非零 Total 就必须原样保留。
	tests := []struct {
		name       string
		tokens     tokenprocessor.TokenValues
		resolution tokenprocessor.HandlerResolution
	}{
		{
			name:       "Claude damaged cache contract",
			tokens:     tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: -1, TotalTokens: 99},
			resolution: mustResolveExecutor(t, "ClaudeExecutor"),
		},
		{
			name:       "Gemini damaged reasoning contract",
			tokens:     tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, ReasoningTokens: -1, TotalTokens: 99},
			resolution: mustResolveExecutor(t, "GeminiExecutor"),
		},
		{
			name:       "Responses damaged cache contract",
			tokens:     tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, CacheReadTokens: -1, TotalTokens: 99},
			resolution: mustResolveExecutor(t, "CodexExecutor"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, test.resolution)
			if result.Tokens.TotalTokens != 99 || hasAction(result, "correct_nonzero_total") {
				t.Fatalf("damaged contract must preserve nonzero Total, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
			}
		})
	}
}

func TestClampedTotalDoesNotCreateZeroTotalFallbackEvidence(t *testing.T) {
	// 负 Total 清零只是在阻止坏值入库，不能把这个人工零值当成“上游漏填 Total”的证据。
	result := tokenprocessor.Process(
		tokenprocessor.TokenValues{InputTokens: 10, OutputTokens: 5, TotalTokens: -1},
		mustResolveExecutor(t, "CodexExecutor"),
	)
	if result.Tokens.TotalTokens != 0 || hasAction(result, tokenprocessor.ActionBackfillZeroTotal) || hasAction(result, tokenprocessor.ActionCorrectNonzeroTotal) {
		t.Fatalf("clamped Total must remain zero without reconciliation, got tokens=%+v actions=%+v violations=%+v", result.Tokens, result.Actions, result.Violations)
	}
	if !hasViolation(result, tokenprocessor.ViolationAmbiguousTotalMismatch) {
		t.Fatalf("expected clamped Total to retain an ambiguity violation, got %+v", result.Violations)
	}
}
