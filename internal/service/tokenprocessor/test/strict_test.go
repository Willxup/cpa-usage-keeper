package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestKimiAndUnknownKeepStrictExistingBehavior(t *testing.T) {
	// 这组数字满足 #272，但 Kimi/unknown 没有兼容方言证据，必须保持 Output 和非零 Total。
	tokens := tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3, CachedTokens: 5, TotalTokens: 21}
	for _, resolution := range []tokenprocessor.HandlerResolution{
		mustResolveExecutor(t, "KimiExecutor"),
		mustResolveIdentity(t, "", "custom-provider"),
	} {
		result := tokenprocessor.Process(tokens, resolution)
		if result.Tokens.OutputTokens != 7 || result.Tokens.TotalTokens != 21 || result.Tokens.CacheReadTokens != 5 {
			t.Fatalf("expected strict pass-through with cache alias, got %+v", result.Tokens)
		}
		if hasAction(result, "apply_issue272_reasoning_fold") {
			t.Fatalf("strict handler must never execute #272, got %+v", result.Actions)
		}
	}
}

func TestStrictHandlerKeepsExistingZeroOnlyTotalFallback(t *testing.T) {
	// 现有规则先尝试 Input+Output；仍为零时才使用 cache read 兼容值。
	parent := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7}, mustResolveExecutor(t, "KimiExecutor"))
	if parent.Tokens.TotalTokens != 18 {
		t.Fatalf("expected zero Total to use input+output, got %+v", parent.Tokens)
	}

	cacheOnly := tokenprocessor.Process(tokenprocessor.TokenValues{CachedTokens: 5}, mustResolveExecutor(t, "KimiExecutor"))
	if cacheOnly.Tokens.CacheReadTokens != 5 || cacheOnly.Tokens.TotalTokens != 5 {
		t.Fatalf("expected zero parent values to fall back to cache read, got %+v", cacheOnly.Tokens)
	}
}

func TestStrictZeroOnlyFallbackRejectsTotalClampedToZero(t *testing.T) {
	// zero-only fallback 依赖“原 Total 真实为零”；负数 clamp 产生的零不能获得旧兼容规则权限。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, TotalTokens: -1}, mustResolveExecutor(t, "KimiExecutor"))
	if result.Tokens.TotalTokens != 0 || hasAction(result, "backfill_zero_total") {
		t.Fatalf("expected damaged Total to remain preserved, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected damaged strict Total to be ambiguous, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}
}
