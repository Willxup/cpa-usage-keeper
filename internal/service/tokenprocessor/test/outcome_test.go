package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestTokenOutcomeKeepsActionsButSummarizesTheMostSevereResult(t *testing.T) {
	// clamp 已完成，但 Input 损坏使 Total 无法纠正；action 与 violation 都要保留，outcome 只取 ambiguous。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: -1, OutputTokens: 7, CachedTokens: 3, TotalTokens: 99}, mustResolveExecutor(t, "CodexExecutor"))
	if !hasAction(result, "clamp_negative_token") || !hasAction(result, "backfill_cache_read_alias") {
		t.Fatalf("expected all independent actions to remain, got %+v", result.Actions)
	}
	if len(result.Violations) == 0 || result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected unresolved damage to dominate outcome, got violations=%+v outcome=%q", result.Violations, result.Outcome)
	}
}

func TestClampWithoutRemainingUncertaintyIsCorrected(t *testing.T) {
	// 与 Total 推导无关的 legacy cached 负值可以安全清零；没有剩余 violation 时 outcome 才是 corrected。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 11, OutputTokens: 7, CachedTokens: -3, TotalTokens: 18}, mustResolveExecutor(t, "KimiExecutor"))
	if result.Tokens.CachedTokens != 0 || !hasAction(result, "clamp_negative_token") || len(result.Violations) != 0 || result.Outcome != tokenprocessor.TokenOutcomeCorrected {
		t.Fatalf("expected isolated clamp to be corrected, got %+v", result)
	}
}
