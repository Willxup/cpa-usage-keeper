package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestClaudeExecutorKeepsExistingInputAndCachedContracts(t *testing.T) {
	// CPA Claude parser 的 Input 不含 read/creation；Keeper 必须合并，并在 handler 最后保持 cached=read。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         100,
		OutputTokens:        20,
		CachedTokens:        99,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
		TotalTokens:         120,
	}, mustResolveExecutor(t, "ClaudeExecutor"))

	if result.Tokens.InputTokens != 115 || result.Tokens.OutputTokens != 20 || result.Tokens.CachedTokens != 10 || result.Tokens.CacheReadTokens != 10 || result.Tokens.CacheCreationTokens != 5 || result.Tokens.TotalTokens != 135 {
		t.Fatalf("expected Claude canonical input/cache values, got %+v", result.Tokens)
	}
	if !hasAction(result, "normalize_claude_input") || !hasAction(result, "normalize_claude_cached_alias") || !hasAction(result, "correct_nonzero_total") {
		t.Fatalf("expected Claude normalization actions, got %+v", result.Actions)
	}
	if !hasViolation(result, tokenprocessor.ViolationCacheReadAliasConflict) {
		t.Fatalf("expected known Claude cached/read disagreement to be recorded, got %+v", result.Violations)
	}
}

func TestClaudeIdentityDoesNotUpgradeCachedReadDisagreementToContractConflict(t *testing.T) {
	// identity 只是历史处理提示，不能证明本次 CPA parser 承诺 cached 与 read 应当相等。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         100,
		OutputTokens:        20,
		CachedTokens:        99,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
		TotalTokens:         135,
	}, mustResolveIdentity(t, "", "claude"))

	// 原有 Claude 兼容规则仍以明确 read 为准并完成 Input fold，但不新增强合同冲突。
	if result.Tokens.InputTokens != 115 || result.Tokens.CachedTokens != 10 || result.Tokens.CacheReadTokens != 10 || result.Tokens.TotalTokens != 135 {
		t.Fatalf("expected identity Claude to preserve existing normalization, got %+v", result.Tokens)
	}
	if hasViolation(result, tokenprocessor.ViolationCacheReadAliasConflict) {
		t.Fatalf("identity hint must not create a parser contract conflict, got %+v", result.Violations)
	}
}

func TestClaudeIdentityKeepsHistoricalInputFoldWithoutCachedBackfill(t *testing.T) {
	// 没有 executor 的旧 Claude 事件仍要合并显式 creation，但不能从 legacy cached 反推 read。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         100,
		OutputTokens:        20,
		CachedTokens:        30,
		CacheReadTokens:     0,
		CacheCreationTokens: 10,
		TotalTokens:         130,
	}, mustResolveIdentity(t, "", "claude"))

	if result.Tokens.InputTokens != 110 || result.Tokens.CacheReadTokens != 0 || result.Tokens.CachedTokens != 0 || result.Tokens.CacheCreationTokens != 10 {
		t.Fatalf("expected identity Claude to trust only explicit read/creation, got %+v", result.Tokens)
	}
}

func TestClaudeInputFoldRejectsClampedCacheDependencies(t *testing.T) {
	// read=-1 清零后不能被当作真实零再与 creation 合并；整个 Claude Input fold 必须拒绝执行。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, CacheReadTokens: -1, CacheCreationTokens: 10, TotalTokens: 130}, mustResolveExecutor(t, "ClaudeExecutor"))
	if result.Tokens.InputTokens != 100 || result.Tokens.CacheReadTokens != 0 || result.Tokens.CachedTokens != 0 || hasAction(result, "normalize_claude_input") {
		t.Fatalf("expected Claude fold to preserve parent Input after clamp, got tokens=%+v actions=%+v", result.Tokens, result.Actions)
	}
	if result.Outcome != tokenprocessor.TokenOutcomeAmbiguous {
		t.Fatalf("expected damaged Claude dependency to be ambiguous, got outcome=%q violations=%+v", result.Outcome, result.Violations)
	}
}
