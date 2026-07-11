package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestNormalizeUsageEventTokensBackfillsCacheReadForConfirmedTypes(t *testing.T) {
	tests := []struct {
		name      string
		usageType string
	}{
		{name: "codex", usageType: "codex"},
		{name: "openai", usageType: "openai"},
		{name: "openai compatible", usageType: "openai-compatible"},
		{name: "openai compatibility underscore", usageType: "openai_compatibility"},
		{name: "openai compatibility hyphen", usageType: "openai-compatibility"},
		{name: "dynamic openai compatible provider", usageType: "openai-compatible-acme"},
		{name: "gemini", usageType: "gemini"},
		{name: "gemini cli", usageType: "gemini-cli"},
		{name: "gemini cli code assist", usageType: "gemini-cli-code-assist"},
		{name: "gemini interactions", usageType: "gemini-interactions"},
		{name: "vertex", usageType: "vertex"},
		{name: "antigravity", usageType: "antigravity"},
		{name: "ai studio compact", usageType: "aistudio"},
		{name: "ai studio", usageType: "ai-studio"},
		{name: "kimi", usageType: "kimi"},
		{name: "moonshot", usageType: "moonshot"},
		{name: "xai", usageType: "xai"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:         100,
				OutputTokens:        20,
				CachedTokens:        30,
				CacheReadTokens:     0,
				CacheCreationTokens: 10,
			}, test.usageType)

			if event.CachedTokens != 30 || event.CacheReadTokens != 30 || event.CacheCreationTokens != 10 {
				t.Fatalf("expected %s cached/read aliases to normalize to 30 with write preserved, got %+v", test.usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensPrefersExplicitCacheRead(t *testing.T) {
	event := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:         100,
		OutputTokens:        20,
		CachedTokens:        30,
		CacheReadTokens:     18,
		CacheCreationTokens: 10,
	}, "codex")

	if event.CachedTokens != 30 || event.CacheReadTokens != 18 || event.CacheCreationTokens != 10 {
		t.Fatalf("expected explicit cache read to win without overwriting cached tokens, got %+v", event)
	}
}

func TestNormalizeUsageEventTokensKeepsClaudeOverrideAndBackfillsUnknownTypes(t *testing.T) {
	claude := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:         100,
		CachedTokens:        10,
		CacheReadTokens:     0,
		CacheCreationTokens: 10,
	}, "claude")
	if claude.InputTokens != 110 || claude.CachedTokens != 0 || claude.CacheReadTokens != 0 || claude.CacheCreationTokens != 10 {
		t.Fatalf("expected Claude to keep its dedicated read/write normalization, got %+v", claude)
	}

	custom := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:     100,
		CachedTokens:    30,
		CacheReadTokens: 0,
	}, "custom-plugin")
	if custom.CachedTokens != 30 || custom.CacheReadTokens != 30 {
		t.Fatalf("expected unknown/custom usage to use the shared cache read fallback, got %+v", custom)
	}
}

func TestNormalizeUsageEventTokensUsesCodexStyleOutputForGeminiFamily(t *testing.T) {
	for _, usageType := range []string{"gemini", "vertex", "gemini-cli", "gemini-cli-code-assist", "antigravity", "aistudio", "ai-studio"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    7,
				ReasoningTokens: 3,
				CachedTokens:    5,
				TotalTokens:     21,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
				t.Fatalf("expected %s to normalize to Codex-style output tokens, got %+v", usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensBackfillsTotalWithCodexStyleOutput(t *testing.T) {
	event := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:     11,
		OutputTokens:    7,
		ReasoningTokens: 3,
		CachedTokens:    5,
	}, "gemini")

	if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.TotalTokens != 21 {
		t.Fatalf("expected Gemini missing total to use input plus normalized output, got %+v", event)
	}
}

func TestNormalizeUsageEventTokensDoesNotDoubleCountCodexReasoningWhenTotalMissing(t *testing.T) {
	event := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:     11,
		OutputTokens:    10,
		ReasoningTokens: 3,
		CachedTokens:    5,
	}, "codex")

	if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.TotalTokens != 21 {
		t.Fatalf("expected Codex missing total to use input plus output, got %+v", event)
	}
}

func TestNormalizeUsageEventTokensKeepsAlreadyIncludedOutputWhenTotalMissing(t *testing.T) {
	for _, usageType := range []string{"codex", "openai", "openai-compatible", "openai_compatibility", "custom"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    10,
				ReasoningTokens: 3,
				CachedTokens:    5,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
				t.Fatalf("expected %s to keep Codex/OpenAI-style output tokens, got %+v", usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensDoesNotFoldCodexWhenCompatibilityWouldFold(t *testing.T) {
	event := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:     11,
		OutputTokens:    7,
		ReasoningTokens: 3,
		CachedTokens:    5,
		TotalTokens:     21,
	}, "codex")

	if event.InputTokens != 11 || event.OutputTokens != 7 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
		t.Fatalf("expected codex normalization to keep output unchanged, got %+v", event)
	}
}

func TestNormalizeUsageEventTokensKeepsResponsesOutputForXAI(t *testing.T) {
	event := service.NormalizeUsageEventTokens(entities.UsageEvent{
		InputTokens:     11,
		OutputTokens:    10,
		ReasoningTokens: 3,
		CachedTokens:    5,
	}, "xai")

	if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
		t.Fatalf("expected xAI Responses tokens to keep Codex-style output tokens, got %+v", event)
	}
}

func TestNormalizeUsageEventTokensFoldsGeminiStyleReasoningForOpenAICompatibility(t *testing.T) {
	for _, usageType := range []string{"openai", "openai-compatible", "openai_compatibility"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    7,
				ReasoningTokens: 3,
				CachedTokens:    5,
				TotalTokens:     21,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
				t.Fatalf("expected %s to fold Gemini-style reasoning into output, got %+v", usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensDoesNotFoldOpenAICompatibilityWithoutTotalProof(t *testing.T) {
	for _, usageType := range []string{"openai", "openai-compatible", "openai_compatibility"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    7,
				ReasoningTokens: 3,
				CachedTokens:    5,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 7 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 18 {
				t.Fatalf("expected %s to keep separated reasoning without total proof, got %+v", usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensKeepsCodexStyleOutputForOpenAICompatibility(t *testing.T) {
	for _, usageType := range []string{"openai", "openai-compatible", "openai_compatibility"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    10,
				ReasoningTokens: 3,
				CachedTokens:    5,
				TotalTokens:     21,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
				t.Fatalf("expected %s to keep Codex-style output when reasoning is already included, got %+v", usageType, event)
			}
		})
	}
}

func TestNormalizeUsageEventTokensDoesNotFoldCodexReasoningWhenTotalPresent(t *testing.T) {
	for _, usageType := range []string{"codex"} {
		t.Run(usageType, func(t *testing.T) {
			event := service.NormalizeUsageEventTokens(entities.UsageEvent{
				InputTokens:     11,
				OutputTokens:    10,
				ReasoningTokens: 3,
				CachedTokens:    5,
				TotalTokens:     21,
			}, usageType)

			if event.InputTokens != 11 || event.OutputTokens != 10 || event.ReasoningTokens != 3 || event.CachedTokens != 5 || event.TotalTokens != 21 {
				t.Fatalf("expected %s normalization to keep output unchanged, got %+v", usageType, event)
			}
		})
	}
}
