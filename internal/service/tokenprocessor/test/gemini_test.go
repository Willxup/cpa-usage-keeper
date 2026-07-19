package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestGeminiExecutorAlwaysFoldsSeparatedReasoning(t *testing.T) {
	// 已知 Gemini executor 已证明 candidates 不含 thoughts，因此不再依赖 Total 猜测是否 fold。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     11,
		OutputTokens:    7,
		ReasoningTokens: 3,
		TotalTokens:     21,
	}, mustResolveExecutor(t, "GeminiExecutor"))

	if result.Tokens.OutputTokens != 10 || result.Tokens.TotalTokens != 21 {
		t.Fatalf("expected Gemini output to include reasoning, got %+v", result.Tokens)
	}
	if !hasAction(result, "normalize_gemini_output") || hasAction(result, "apply_issue272_reasoning_fold") {
		t.Fatalf("expected Gemini-owned action and no #272 action, got %+v", result.Actions)
	}
}

func TestGeminiIdentityPreservesExistingFourBranchDecision(t *testing.T) {
	// identity fallback 必须原样保留旧 Gemini reasoning 判断的四种等式分支。
	tests := []struct {
		name           string
		tokens         tokenprocessor.TokenValues
		expectedOutput int64
		expectedTotal  int64
	}{
		{name: "reasoning absent", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, TotalTokens: 18}, expectedOutput: 7, expectedTotal: 18},
		{name: "missing total folds", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3}, expectedOutput: 10, expectedTotal: 21},
		{name: "output already inclusive", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 10, ReasoningTokens: 3, TotalTokens: 21}, expectedOutput: 10, expectedTotal: 21},
		{name: "separated total proof", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: 21}, expectedOutput: 10, expectedTotal: 21},
		{name: "ambiguous total", tokens: tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: 99}, expectedOutput: 7, expectedTotal: 99},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := tokenprocessor.Process(test.tokens, mustResolveIdentity(t, "", "gemini"))
			if result.Tokens.OutputTokens != test.expectedOutput || result.Tokens.TotalTokens != test.expectedTotal {
				t.Fatalf("expected output=%d total=%d, got %+v", test.expectedOutput, test.expectedTotal, result.Tokens)
			}
		})
	}
}

func TestGeminiIdentityDoesNotFoldWhenMissingTotalWasCreatedByClamp(t *testing.T) {
	// 旧规则的 total==0 分支只适用于真实零值；负 Total clamp 后的零不能证明 reasoning 分列。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: -1}, mustResolveIdentity(t, "", "gemini"))
	if result.Tokens.OutputTokens != 7 || result.Tokens.TotalTokens != 0 || hasAction(result, "normalize_gemini_output") || hasAction(result, "backfill_zero_total") {
		t.Fatalf("expected clamped Gemini Total to preserve parent fields, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected clamped Gemini identity event to be ambiguous, got %q", result.Outcome)
	}
}

func TestGeminiExecutorDoesNotFoldReasoningIntoClampedOutput(t *testing.T) {
	// Output=-1 清零后不能再加 reasoning；该 fold 的父字段已经损坏，必须保留清零值并降级。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: -1, ReasoningTokens: 3, TotalTokens: 14}, mustResolveExecutor(t, "GeminiExecutor"))
	if result.Tokens.OutputTokens != 0 || hasAction(result, "normalize_gemini_output") {
		t.Fatalf("expected Gemini fold to reject clamped Output, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected damaged Gemini dependency to be ambiguous, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}
}
