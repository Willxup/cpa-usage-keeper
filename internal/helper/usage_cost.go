package helper

import "cpa-usage-keeper/internal/entities"

// UsageTokenCostInput 是价格计算的最小 token 输入，避免 repository 为事件和聚合行各维护一套公式。
type UsageTokenCostInput struct {
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

type UsageTokenCostBreakdown struct {
	InputCostUSD  float64
	OutputCostUSD float64
	CachedCostUSD float64
	TotalCostUSD  float64
}

// UsageTokenInputRequiresPricing 判断聚合 token 输入是否需要价格表才能给出完整 cost。
func UsageTokenInputRequiresPricing(input UsageTokenCostInput) bool {
	return input.InputTokens > 0 || input.OutputTokens > 0 || input.CachedTokens > 0 || input.CacheReadTokens > 0 || input.CacheCreationTokens > 0
}

// CalculateUsageTokenCostBreakdown 把总费用拆成 input/output/cached 三段，供 Analysis 页面复用同一计价口径。
// OpenAI 风格把 cached_tokens 视为 input_tokens 的子集；Claude 风格把 cache read/write 从已归一化的总 input 中拆回单独价格。
func CalculateUsageTokenCostBreakdown(input UsageTokenCostInput, pricing entities.ModelPriceSetting) UsageTokenCostBreakdown {
	input = clampUsageTokenCostInput(input)
	var breakdown UsageTokenCostBreakdown
	switch pricing.PricingStyle {
	case entities.ModelPricingStyleClaude:
		breakdown = calculateClaudeUsageTokenCostBreakdown(input, pricing)
	default:
		breakdown = calculateOpenAIStyleUsageTokenCostBreakdown(input, pricing)
	}
	return scaleUsageTokenCostBreakdown(breakdown, modelPriceMultiplier(pricing))
}

func calculateOpenAIStyleUsageTokenCostBreakdown(input UsageTokenCostInput, pricing entities.ModelPriceSetting) UsageTokenCostBreakdown {
	inputTokens := input.InputTokens
	outputTokens := input.OutputTokens
	cachedTokens := input.CachedTokens
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	breakdown := UsageTokenCostBreakdown{
		InputCostUSD:  (float64(promptTokens) / 1_000_000.0) * pricing.PromptPricePer1M,
		OutputCostUSD: (float64(outputTokens) / 1_000_000.0) * pricing.CompletionPricePer1M,
		CachedCostUSD: (float64(cachedTokens) / 1_000_000.0) * pricing.CachePricePer1M,
	}
	breakdown.TotalCostUSD = breakdown.InputCostUSD + breakdown.OutputCostUSD + breakdown.CachedCostUSD
	return breakdown
}

func calculateClaudeUsageTokenCostBreakdown(input UsageTokenCostInput, pricing entities.ModelPriceSetting) UsageTokenCostBreakdown {
	normalInputTokens := input.InputTokens - input.CacheReadTokens - input.CacheCreationTokens
	if normalInputTokens < 0 {
		normalInputTokens = 0
	}
	breakdown := UsageTokenCostBreakdown{
		InputCostUSD:  (float64(normalInputTokens) / 1_000_000.0) * pricing.PromptPricePer1M,
		OutputCostUSD: (float64(input.OutputTokens) / 1_000_000.0) * pricing.CompletionPricePer1M,
		CachedCostUSD: (float64(input.CacheReadTokens)/1_000_000.0)*pricing.CachePricePer1M +
			(float64(input.CacheCreationTokens)/1_000_000.0)*pricing.CacheCreationPricePer1M,
	}
	breakdown.TotalCostUSD = breakdown.InputCostUSD + breakdown.OutputCostUSD + breakdown.CachedCostUSD
	return breakdown
}

func clampUsageTokenCostInput(input UsageTokenCostInput) UsageTokenCostInput {
	input.InputTokens = maxInt64(input.InputTokens, 0)
	input.OutputTokens = maxInt64(input.OutputTokens, 0)
	input.CachedTokens = maxInt64(input.CachedTokens, 0)
	input.CacheReadTokens = maxInt64(input.CacheReadTokens, 0)
	input.CacheCreationTokens = maxInt64(input.CacheCreationTokens, 0)
	return input
}

func modelPriceMultiplier(pricing entities.ModelPriceSetting) float64 {
	if pricing.PriceMultiplier == nil {
		return 1
	}
	return *pricing.PriceMultiplier
}

func scaleUsageTokenCostBreakdown(breakdown UsageTokenCostBreakdown, multiplier float64) UsageTokenCostBreakdown {
	breakdown.InputCostUSD *= multiplier
	breakdown.OutputCostUSD *= multiplier
	breakdown.CachedCostUSD *= multiplier
	breakdown.TotalCostUSD *= multiplier
	return breakdown
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
