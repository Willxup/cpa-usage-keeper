package service

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
)

// NormalizeUsageEventTokens 是 Redis usage 入库前的唯一 token 口径归一化入口。
// Keeper 统一按 Codex/OpenAI Responses 格式存储 token：input_tokens 包含 cached_tokens，
// output_tokens 包含 reasoning_tokens；cached_tokens 和 reasoning_tokens 只是各自的明细子项。
// 后续统计、成本和前端可见 token 展示都依赖这个统一口径：visible input = input - cached，
// visible output = output - reasoning。
func NormalizeUsageEventTokens(event entities.UsageEvent, usageType string) entities.UsageEvent {
	tokens := normalizeUsageTokensByType(usageEventTokenStats(event), usageType)
	event.InputTokens = tokens.InputTokens
	event.OutputTokens = tokens.OutputTokens
	event.ReasoningTokens = tokens.ReasoningTokens
	event.CachedTokens = tokens.CachedTokens
	event.CacheReadTokens = tokens.CacheReadTokens
	event.CacheCreationTokens = tokens.CacheCreationTokens
	event.TotalTokens = tokens.TotalTokens
	return event
}

func usageEventTokenStats(event entities.UsageEvent) dto.TokenStats {
	return dto.TokenStats{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

func normalizeUsageTokensByType(tokens dto.TokenStats, usageType string) dto.TokenStats {
	switch strings.ToLower(strings.TrimSpace(usageType)) {
	case "claude", "anthropic":
		return normalizeClaudeTokens(tokens)
	case "gemini", "vertex", "gemini-cli", "gemini-cli-code-assist", "aistudio", "ai-studio":
		return normalizeGeminiTokens(tokens)
	case "antigravity":
		return normalizeAntigravityTokens(tokens)
	case "kimi", "moonshot":
		return normalizeKimiTokens(tokens)
	case "xai":
		return normalizeXAIStyleTokens(tokens)
	case "openai", "codex":
		return normalizeOpenAIStyleTokens(tokens)
	case "openai-compatible", "openai_compatibility":
		return normalizeOpenAICompatibilityTokens(tokens)
	default:
		return normalizeDefaultTokens(tokens)
	}
}

func normalizeClaudeTokens(tokens dto.TokenStats) dto.TokenStats {
	tokens = clampTokenStats(tokens)
	// Claude 官方 input_tokens 不含 cache read/write；按 Codex 格式入库前合并为总 input。
	tokens.InputTokens = tokens.InputTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens
	tokens.CachedTokens = tokens.CacheReadTokens
	return fillCodexStyleTotalTokens(tokens)
}

func normalizeOpenAIStyleTokens(tokens dto.TokenStats) dto.TokenStats {
	tokens = clampTokenStats(tokens)
	// Real OpenAI and Codex Responses usage already includes reasoning_tokens in output_tokens.
	return fillCodexStyleTotalTokens(tokens)
}

func normalizeOpenAICompatibilityTokens(tokens dto.TokenStats) dto.TokenStats {
	tokens = clampTokenStats(tokens)
	// OpenAI-compatible protocol does not guarantee OpenAI token semantics. CPA may route
	// Gemini-family models through OpenAICompatExecutor where output_tokens and
	// reasoning_tokens are reported separately. Fold when arithmetic proves reasoning
	// is not already included in output.
	if tokens.ReasoningTokens > 0 &&
		tokens.TotalTokens > 0 &&
		tokens.InputTokens+tokens.OutputTokens+tokens.ReasoningTokens == tokens.TotalTokens {
		tokens.OutputTokens += tokens.ReasoningTokens
	}
	return fillCodexStyleTotalTokens(tokens)
}

func normalizeGeminiTokens(tokens dto.TokenStats) dto.TokenStats {
	tokens = clampTokenStats(tokens)
	// Gemini 家族的 CPA 原始映射是 output=candidatesTokenCount、reasoning=thoughtsTokenCount；
	// Keeper 入库统一成 Codex 格式，因此 output 需要包含 thinking/reasoning。
	if shouldFoldReasoningIntoOutput(tokens) {
		tokens.OutputTokens += tokens.ReasoningTokens
	}
	return fillCodexStyleTotalTokens(tokens)
}

func normalizeAntigravityTokens(tokens dto.TokenStats) dto.TokenStats {
	return normalizeGeminiTokens(tokens)
}

func normalizeKimiTokens(tokens dto.TokenStats) dto.TokenStats {
	return normalizeOpenAIStyleTokens(tokens)
}

func normalizeXAIStyleTokens(tokens dto.TokenStats) dto.TokenStats {
	tokens = clampTokenStats(tokens)
	// xAI 当前由 CPA XAIExecutor 调用 /v1/responses，usage 是 OpenAI Responses 风格：
	// input_tokens 已包含 input_tokens_details.cached_tokens，output_tokens 已包含 reasoning_tokens。
	// 后续如果 xAI 切到其他 usage 口径，只需要收敛修改这个分支。
	return fillCodexStyleTotalTokens(tokens)
}

func normalizeDefaultTokens(tokens dto.TokenStats) dto.TokenStats {
	return normalizeOpenAIStyleTokens(tokens)
}

func shouldFoldReasoningIntoOutput(tokens dto.TokenStats) bool {
	if tokens.ReasoningTokens <= 0 {
		return false
	}
	if tokens.TotalTokens == 0 {
		return true
	}
	if tokens.InputTokens+tokens.OutputTokens == tokens.TotalTokens {
		return false
	}
	return tokens.InputTokens+tokens.OutputTokens+tokens.ReasoningTokens == tokens.TotalTokens
}

func fillCodexStyleTotalTokens(tokens dto.TokenStats) dto.TokenStats {
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.CachedTokens
	}
	return tokens
}

func clampTokenStats(tokens dto.TokenStats) dto.TokenStats {
	tokens.InputTokens = max(tokens.InputTokens, 0)
	tokens.OutputTokens = max(tokens.OutputTokens, 0)
	tokens.ReasoningTokens = max(tokens.ReasoningTokens, 0)
	tokens.CachedTokens = max(tokens.CachedTokens, 0)
	tokens.CacheReadTokens = max(tokens.CacheReadTokens, 0)
	tokens.CacheCreationTokens = max(tokens.CacheCreationTokens, 0)
	tokens.TotalTokens = max(tokens.TotalTokens, 0)
	return tokens
}

func max(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
