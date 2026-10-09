package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestMetaAndDevinIdentityFallbacksRemainHints(t *testing.T) {
	meta, err := tokenprocessor.ResolveIdentity("", "meta")
	if err != nil {
		t.Fatalf("resolve Meta identity fallback: %v", err)
	}
	if meta.HandlerID() != tokenprocessor.HandlerResponsesInclusive || meta.EvidenceSource() != tokenprocessor.EvidenceIdentity || meta.EvidenceStrength() != tokenprocessor.EvidenceIdentityHint {
		t.Fatalf("expected Meta identity fallback to remain a hint, got handler=%q source=%q strength=%q", meta.HandlerID(), meta.EvidenceSource(), meta.EvidenceStrength())
	}

	devin, err := tokenprocessor.ResolveIdentity("", "devin")
	if err != nil {
		t.Fatalf("resolve Devin identity fallback: %v", err)
	}
	if devin.HandlerID() != tokenprocessor.HandlerStrictPassThrough || devin.EvidenceSource() != tokenprocessor.EvidenceIdentity || devin.EvidenceStrength() != tokenprocessor.EvidenceIdentityHint {
		t.Fatalf("expected Devin identity fallback to remain a hint, got handler=%q source=%q strength=%q", devin.HandlerID(), devin.EvidenceSource(), devin.EvidenceStrength())
	}
}

func TestMetaAndDevinExactExecutorsWinOverIdentityAliases(t *testing.T) {
	meta, err := tokenprocessor.ResolveIdentity("MetaExecutor", "devin")
	if err != nil {
		t.Fatalf("resolve Meta executor with conflicting Devin identity: %v", err)
	}
	if meta.HandlerID() != tokenprocessor.HandlerResponsesInclusive || meta.EvidenceSource() != tokenprocessor.EvidenceExecutor {
		t.Fatalf("Meta executor must win over identity alias, got handler=%q source=%q", meta.HandlerID(), meta.EvidenceSource())
	}

	devin, err := tokenprocessor.ResolveIdentity("DevinExecutor", "meta")
	if err != nil {
		t.Fatalf("resolve Devin executor with conflicting Meta identity: %v", err)
	}
	if devin.HandlerID() != tokenprocessor.HandlerStrictPassThrough || devin.EvidenceSource() != tokenprocessor.EvidenceExecutor {
		t.Fatalf("Devin executor must win over identity alias, got handler=%q source=%q", devin.HandlerID(), devin.EvidenceSource())
	}
}

func TestMetaAndDevinLegacyIdentityPayloadsKeepVerifiedNormalization(t *testing.T) {
	// 旧 CPA payload 缺少 executor_type 时，Auth File type 只走已验证 identity hint，仍不改变权限边界。
	meta := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     11,
		OutputTokens:    10,
		ReasoningTokens: 3,
		TotalTokens:     0,
	}, mustResolveIdentity(t, "", "meta"))
	if meta.Tokens.OutputTokens != 10 || meta.Tokens.TotalTokens != 21 {
		t.Fatalf("legacy Meta identity must keep Responses zero-only normalization, got %+v", meta.Tokens)
	}

	devin := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     6,
		OutputTokens:    6,
		ReasoningTokens: 3,
		TotalTokens:     15,
	}, mustResolveIdentity(t, "", "devin"))
	if devin.Tokens.OutputTokens != 6 || devin.Tokens.TotalTokens != 15 {
		t.Fatalf("legacy Devin identity must preserve scalar usage conservatively, got %+v", devin.Tokens)
	}
}

func TestMetaExecutorUsesResponsesInclusiveContract(t *testing.T) {
	// CPA Meta Responses 的 input/output 已分别包含 cache/reasoning；Keeper 不能再次折叠任何子项。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         100,
		OutputTokens:        20,
		ReasoningTokens:     5,
		CachedTokens:        30,
		CacheReadTokens:     30,
		CacheCreationTokens: 10,
		TotalTokens:         120,
	}, mustResolveExecutor(t, "MetaExecutor"))

	if result.Tokens.InputTokens != 100 || result.Tokens.OutputTokens != 20 ||
		result.Tokens.ReasoningTokens != 5 || result.Tokens.TotalTokens != 120 {
		t.Fatalf("expected Meta Responses values to stay inclusive, got %+v", result.Tokens)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("complete Meta Responses usage must not be ambiguous, got %+v", result.Violations)
	}
}

func TestMetaExecutorDoesNotAddReasoningToResponsesTotal(t *testing.T) {
	// Responses total 的零值兼容只使用 Input+Output；Reasoning 已包含在 Output 语义中。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     11,
		OutputTokens:    10,
		ReasoningTokens: 3,
		TotalTokens:     0,
	}, mustResolveExecutor(t, "MetaExecutor"))

	if result.Tokens.OutputTokens != 10 || result.Tokens.TotalTokens != 21 {
		t.Fatalf("expected Meta zero Total to become 21 without reasoning duplication, got %+v", result.Tokens)
	}
	if !hasAction(result, tokenprocessor.ActionBackfillZeroTotal) {
		t.Fatalf("expected existing zero Total compatibility action, got %+v", result.Actions)
	}
}

func TestDevinExecutorUsesStrictInteractionsBoundary(t *testing.T) {
	// CPA Interactions 的完整合同可观察到分列 reasoning，但 Redis scalar DTO 无法证明 partial/error 完整性。
	result := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:         6, // total_input=2 + total_tool_use=4
		OutputTokens:        6, // total_output，不含 thought
		ReasoningTokens:     3, // total_thought
		CachedTokens:        1,
		CacheReadTokens:     1,
		CacheCreationTokens: 0,
		TotalTokens:         15,
	}, mustResolveExecutor(t, "DevinExecutor"))

	if result.Tokens.InputTokens != 6 || result.Tokens.OutputTokens != 6 ||
		result.Tokens.ReasoningTokens != 3 || result.Tokens.TotalTokens != 15 {
		t.Fatalf("expected Devin strict pass-through, got %+v", result.Tokens)
	}
	if hasAction(result, tokenprocessor.ActionNormalizeGeminiOutput) || hasAction(result, tokenprocessor.ActionCorrectNonzeroTotal) {
		t.Fatalf("Devin strict handler must not fold or correct nonzero Total, got actions=%+v", result.Actions)
	}
}

func TestDevinExecutorKeepsExistingSafeTotalReconciliation(t *testing.T) {
	// 完整 Interactions 合同下，Total 已含 thought；错误非零 Total 沿用既有 Gemini canonical correction。
	corrected := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     6,
		OutputTokens:    6,
		ReasoningTokens: 3,
		TotalTokens:     99,
	}, mustResolveExecutor(t, "DevinExecutor"))
	if corrected.Tokens.OutputTokens != 6 || corrected.Tokens.TotalTokens != 99 ||
		hasAction(corrected, tokenprocessor.ActionCorrectNonzeroTotal) {
		t.Fatalf("expected inconsistent Devin Total to remain preserved, got tokens=%+v actions=%+v", corrected.Tokens, corrected.Actions)
	}

	// partial usage 没有 Total 时只折叠已上报 reasoning，不引入其它 provider 的推断。
	partial := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     6,
		ReasoningTokens: 3,
	}, mustResolveExecutor(t, "DevinExecutor"))
	if partial.Tokens.OutputTokens != 0 || partial.Tokens.TotalTokens != 6 {
		t.Fatalf("expected partial Devin usage to use strict zero-only fallback, got %+v", partial.Tokens)
	}

	partialWithTotal := tokenprocessor.Process(tokenprocessor.TokenValues{
		InputTokens:     6,
		ReasoningTokens: 3,
		TotalTokens:     15,
	}, mustResolveExecutor(t, "DevinExecutor"))
	if partialWithTotal.Tokens.OutputTokens != 0 || partialWithTotal.Tokens.TotalTokens != 15 {
		t.Fatalf("partial Devin usage must preserve authoritative nonzero Total, got %+v", partialWithTotal.Tokens)
	}
}
