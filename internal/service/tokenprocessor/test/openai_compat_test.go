package tokenprocessor_test

import (
	"math"
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestIssue272FoldsSeparatedReasoningOnlyInsideOpenAICompatibility(t *testing.T) {
	// #272 依赖原始 Total 证明 Gemini-compatible reasoning 与 Output 分列，executor 与历史 identity 都要保留该修复。
	for _, resolution := range []tokenprocessor.HandlerResolution{
		mustResolveExecutor(t, "OpenAICompatExecutor"),
		mustResolveIdentity(t, "", "openai"),
	} {
		result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 1000, OutputTokens: 20, ReasoningTokens: 50, TotalTokens: 1070}, resolution)
		if result.Tokens.OutputTokens != 70 || result.Tokens.TotalTokens != 1070 {
			t.Fatalf("expected #272 to fold separated reasoning, got %+v", result.Tokens)
		}
		if !hasAction(result, "apply_issue272_reasoning_fold") {
			t.Fatalf("expected #272 action, got %+v", result.Actions)
		}
	}
}

func TestIssue272DoesNotDoubleFoldInclusiveOutput(t *testing.T) {
	// 标准 inclusive 形态已满足 Input+Output=Total，不能再次加 reasoning。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 1000, OutputTokens: 70, ReasoningTokens: 50, TotalTokens: 1070}, mustResolveExecutor(t, "OpenAICompatExecutor"))
	if result.Tokens.OutputTokens != 70 || hasAction(result, "apply_issue272_reasoning_fold") {
		t.Fatalf("expected inclusive OpenAI-compatible output to stay unchanged, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
}

func TestIssue272RequiresARealPositiveTotalProof(t *testing.T) {
	// 原 Total 确实为零时没有证据证明 reasoning 与 Output 分列；executor 和历史 identity 都只能保留 Output，再执行旧零 Total 补值。
	for _, resolution := range []tokenprocessor.HandlerResolution{
		mustResolveExecutor(t, "OpenAICompatExecutor"),
		mustResolveIdentity(t, "", "openai"),
	} {
		result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3}, resolution)
		if result.Tokens.OutputTokens != 7 || result.Tokens.TotalTokens != 18 {
			t.Fatalf("expected missing Total to preserve separated Output and backfill 18, got %+v", result.Tokens)
		}
		if hasAction(result, "apply_issue272_reasoning_fold") || !hasAction(result, tokenprocessor.ActionBackfillZeroTotal) {
			t.Fatalf("expected only the existing zero Total fallback, got %+v", result.Actions)
		}
	}
}

func TestIssue272RejectsEquationsCreatedByNegativeClamp(t *testing.T) {
	// Input=-5 清零后会伪造 0+7+3=10；规则必须记住 Input 曾损坏并拒绝命中。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: -5, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: 10}, mustResolveExecutor(t, "OpenAICompatExecutor"))
	if result.Tokens.InputTokens != 0 || result.Tokens.OutputTokens != 7 || !hasAction(result, "clamp_negative_token") || hasAction(result, "apply_issue272_reasoning_fold") {
		t.Fatalf("expected clamp without #272 fold, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
}

func TestIssue272DoesNotTreatClampedNegativeTotalAsMissingTotal(t *testing.T) {
	// raw Total=-1 被清零不等于 provider 没返回 Total；#272 与 zero-only fallback 都必须拒绝使用这个伪零值。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, TotalTokens: -1}, mustResolveExecutor(t, "OpenAICompatExecutor"))
	if result.Tokens.TotalTokens != 0 || result.Tokens.OutputTokens != 7 || hasAction(result, "backfill_zero_total") || hasAction(result, "apply_issue272_reasoning_fold") {
		t.Fatalf("expected clamped Total to stay preserved without compatibility match, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected damaged Total to remain ambiguous, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}
}

func TestIssue272UsesSafeArithmetic(t *testing.T) {
	// 溢出不能回绕成新的等式，也不能让单条事件处理失败。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: math.MaxInt64, OutputTokens: 1, ReasoningTokens: 1, TotalTokens: 1}, mustResolveExecutor(t, "OpenAICompatExecutor"))
	if result.Tokens.OutputTokens != 1 || hasAction(result, "apply_issue272_reasoning_fold") {
		t.Fatalf("expected overflow to reject #272, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if !hasViolation(result, "token_arithmetic_overflow") {
		t.Fatalf("expected overflow violation, got %+v", result.Violations)
	}
}
