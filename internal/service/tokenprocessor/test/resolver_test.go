package tokenprocessor_test

import (
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestResolveExecutorUsesCPAParserContracts(t *testing.T) {
	// 每个表项都对应 CPA reporter 实际写入的 Go executor 类型，防止新增 registry 时漏掉独立 definition。
	tests := []struct {
		name      string
		executor  string
		handlerID tokenprocessor.HandlerID
	}{
		{name: "claude", executor: "ClaudeExecutor", handlerID: tokenprocessor.HandlerClaude},
		{name: "gemini", executor: "GeminiExecutor", handlerID: tokenprocessor.HandlerGemini},
		{name: "gemini vertex", executor: "GeminiVertexExecutor", handlerID: tokenprocessor.HandlerGemini},
		{name: "gemini cli historical", executor: "GeminiCLIExecutor", handlerID: tokenprocessor.HandlerGemini},
		{name: "ai studio", executor: "AIStudioExecutor", handlerID: tokenprocessor.HandlerGemini},
		{name: "antigravity", executor: "AntigravityExecutor", handlerID: tokenprocessor.HandlerGemini},
		{name: "codex", executor: "CodexExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "codex websocket", executor: "CodexWebsocketsExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "codex auto", executor: "CodexAutoExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "xai", executor: "XAIExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "xai websocket", executor: "XAIWebsocketsExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "xai auto", executor: "XAIAutoExecutor", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{name: "kimi", executor: "KimiExecutor", handlerID: tokenprocessor.HandlerStrictPassThrough},
		{name: "openai compatibility", executor: "OpenAICompatExecutor", handlerID: tokenprocessor.HandlerOpenAICompatibility},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// 大小写和首尾空格不属于协议语义，resolver 必须统一后再匹配 CPA 类型名。
			resolution, err := tokenprocessor.ResolveExecutor("  " + test.executor + "  ")
			if err != nil {
				t.Fatalf("ResolveExecutor returned error: %v", err)
			}
			if resolution.HandlerID() != test.handlerID {
				t.Fatalf("expected %q to use handler %q, got %q", test.executor, test.handlerID, resolution.HandlerID())
			}
			if resolution.EvidenceSource() != tokenprocessor.EvidenceExecutor || resolution.EvidenceStrength() != tokenprocessor.EvidenceParserContract {
				t.Fatalf("expected %q to carry executor parser contract, got source=%q strength=%q", test.executor, resolution.EvidenceSource(), resolution.EvidenceStrength())
			}
			if resolution.NeedsIdentity() {
				t.Fatalf("known executor %q must not depend on identity lookup", test.executor)
			}
		})
	}
}

func TestResolveIdentityUsesExistingFallbackAliases(t *testing.T) {
	// identity 只是旧事件和未知 executor 的 fallback hint，不能升级为 CPA parser contract。
	tests := []struct {
		identity  string
		handlerID tokenprocessor.HandlerID
	}{
		{identity: "claude", handlerID: tokenprocessor.HandlerClaude},
		{identity: "anthropic", handlerID: tokenprocessor.HandlerClaude},
		{identity: "gemini", handlerID: tokenprocessor.HandlerGemini},
		{identity: "vertex", handlerID: tokenprocessor.HandlerGemini},
		{identity: "gemini-cli", handlerID: tokenprocessor.HandlerGemini},
		{identity: "gemini-cli-code-assist", handlerID: tokenprocessor.HandlerGemini},
		{identity: "gemini-interactions", handlerID: tokenprocessor.HandlerGemini},
		{identity: "aistudio", handlerID: tokenprocessor.HandlerGemini},
		{identity: "ai-studio", handlerID: tokenprocessor.HandlerGemini},
		{identity: "antigravity", handlerID: tokenprocessor.HandlerGemini},
		{identity: "codex", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{identity: "xai", handlerID: tokenprocessor.HandlerResponsesInclusive},
		{identity: "kimi", handlerID: tokenprocessor.HandlerStrictPassThrough},
		{identity: "moonshot", handlerID: tokenprocessor.HandlerStrictPassThrough},
		{identity: "openai", handlerID: tokenprocessor.HandlerOpenAICompatibility},
		{identity: "openai-compatible", handlerID: tokenprocessor.HandlerOpenAICompatibility},
		{identity: "openai_compatibility", handlerID: tokenprocessor.HandlerOpenAICompatibility},
		{identity: "openai-compatibility", handlerID: tokenprocessor.HandlerOpenAICompatibility},
		{identity: "openai-compatible-acme", handlerID: tokenprocessor.HandlerOpenAICompatibility},
	}

	for _, test := range tests {
		t.Run(test.identity, func(t *testing.T) {
			// 空 executor 明确要求 resolver 只使用 identity fallback。
			resolution, err := tokenprocessor.ResolveIdentity("", test.identity)
			if err != nil {
				t.Fatalf("ResolveIdentity returned error: %v", err)
			}
			if resolution.HandlerID() != test.handlerID {
				t.Fatalf("expected identity %q to use handler %q, got %q", test.identity, test.handlerID, resolution.HandlerID())
			}
			if resolution.EvidenceSource() != tokenprocessor.EvidenceIdentity || resolution.EvidenceStrength() != tokenprocessor.EvidenceIdentityHint {
				t.Fatalf("expected identity %q to remain a hint, got source=%q strength=%q", test.identity, resolution.EvidenceSource(), resolution.EvidenceStrength())
			}
		})
	}
}

func TestResolveIdentityKeepsExecutorPriorityForKimiClaudeDelegation(t *testing.T) {
	// CPA 的 Kimi Claude 入站会由 ClaudeExecutor 上报；executor 与 identity 不同是合法委托，不是冲突。
	resolution, err := tokenprocessor.ResolveIdentity("ClaudeExecutor", "kimi")
	if err != nil {
		t.Fatalf("ResolveIdentity returned error: %v", err)
	}
	if resolution.HandlerID() != tokenprocessor.HandlerClaude {
		t.Fatalf("expected ClaudeExecutor to win over Kimi identity, got %q", resolution.HandlerID())
	}
	if resolution.EvidenceSource() != tokenprocessor.EvidenceExecutor || resolution.EvidenceStrength() != tokenprocessor.EvidenceParserContract {
		t.Fatalf("expected delegated event to keep executor contract, got source=%q strength=%q", resolution.EvidenceSource(), resolution.EvidenceStrength())
	}
}

func TestResolveExecutorRequiresIdentityOnlyForMissingOrUnknownTypes(t *testing.T) {
	// 只有缺失、CPA 显式 unknown 或未来未注册类型才允许进入数据库 identity 查询。
	tests := []struct {
		executor          string
		isUnknownExecutor bool
	}{
		// 空值是历史数据缺字段，不是新的 CPA 执行器名称。
		{executor: "", isUnknownExecutor: false},
		// unknown 是 CPA 已知的固定占位值，需要查 identity，但不应产生新类型告警。
		{executor: "unknown", isUnknownExecutor: false},
		// 真正未注册的非空名称才是需要维护 registry 的新执行器。
		{executor: "FutureExecutor", isUnknownExecutor: true},
	}
	for _, test := range tests {
		t.Run(test.executor, func(t *testing.T) {
			resolution, err := tokenprocessor.ResolveExecutor(test.executor)
			if err != nil {
				t.Fatalf("ResolveExecutor returned error: %v", err)
			}
			if !resolution.NeedsIdentity() {
				t.Fatalf("executor %q must request identity fallback", test.executor)
			}
			if resolution.HandlerID() != tokenprocessor.HandlerStrictPassThrough {
				t.Fatalf("unresolved executor %q must remain strict until identity lookup, got %q", test.executor, resolution.HandlerID())
			}
			if resolution.UnknownExecutor() != test.isUnknownExecutor {
				t.Fatalf("executor %q unknown observation = %t, want %t", test.executor, resolution.UnknownExecutor(), test.isUnknownExecutor)
			}
		})
	}
}

func TestResolveIdentityUsesOnlyTheDocumentedOpenAICompatibilityPrefix(t *testing.T) {
	// openai-compatible-* 是唯一允许的 prefix matcher；其它相似名字不能靠猜测获得协议规则。
	openAI, err := tokenprocessor.ResolveIdentity("", "openai-compatible-acme")
	if err != nil {
		t.Fatalf("resolve OpenAI compatibility prefix: %v", err)
	}
	if openAI.HandlerID() != tokenprocessor.HandlerOpenAICompatibility {
		t.Fatalf("expected documented prefix to use OpenAI compatibility, got %q", openAI.HandlerID())
	}

	unknown, err := tokenprocessor.ResolveIdentity("", "claude-acme")
	if err != nil {
		t.Fatalf("resolve undocumented prefix: %v", err)
	}
	if unknown.HandlerID() != tokenprocessor.HandlerStrictPassThrough || unknown.EvidenceSource() != tokenprocessor.EvidenceDefault {
		t.Fatalf("expected undocumented prefix to stay strict default, got handler=%q source=%q", unknown.HandlerID(), unknown.EvidenceSource())
	}
}
