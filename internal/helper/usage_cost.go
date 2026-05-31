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

// UsageEventRequiresPricing 判断事件是否包含需要价格表解释的计费 token。
func UsageEventRequiresPricing(event entities.UsageEvent) bool {
	return UsageTokenInputRequiresPricing(UsageTokenCostInput{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
	})
}

// UsageTokenInputRequiresPricing 判断聚合 token 输入是否需要价格表才能给出完整 cost。
func UsageTokenInputRequiresPricing(input UsageTokenCostInput) bool {
	return input.InputTokens > 0 || input.OutputTokens > 0 || input.CachedTokens > 0 || input.CacheReadTokens > 0 || input.CacheCreationTokens > 0
}

// CalculateUsageEventCost 复用通用 token 公式计算单条 usage_event 的费用。
func CalculateUsageEventCost(event entities.UsageEvent, pricing entities.ModelPriceSetting) float64 {
	return CalculateUsageTokenCost(UsageTokenCostInput{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
	}, pricing)
}

// CalculateUsageTokenCost 按当前价格风格计算费用。
// OpenAI 风格把 cached_tokens 视为 input_tokens 的子集；Claude 风格把 cache read/write 从已归一化的总 input 中拆回单独价格。
func CalculateUsageTokenCost(input UsageTokenCostInput, pricing entities.ModelPriceSetting) float64 {
	input = clampUsageTokenCostInput(input)
	switch pricing.PricingStyle {
	case entities.ModelPricingStyleClaude:
		return calculateClaudeUsageTokenCost(input, pricing)
	default:
		return calculateOpenAIStyleUsageTokenCost(input, pricing)
	}
}

func calculateOpenAIStyleUsageTokenCost(input UsageTokenCostInput, pricing entities.ModelPriceSetting) float64 {
	inputTokens := input.InputTokens
	outputTokens := input.OutputTokens
	cachedTokens := input.CachedTokens
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(outputTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTokens)/1_000_000.0)*pricing.CachePricePer1M
}

func calculateClaudeUsageTokenCost(input UsageTokenCostInput, pricing entities.ModelPriceSetting) float64 {
	normalInputTokens := input.InputTokens - input.CacheReadTokens - input.CacheCreationTokens
	if normalInputTokens < 0 {
		normalInputTokens = 0
	}
	return (float64(normalInputTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(input.OutputTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(input.CacheReadTokens)/1_000_000.0)*pricing.CachePricePer1M +
		(float64(input.CacheCreationTokens)/1_000_000.0)*pricing.CacheCreationPricePer1M
}

func clampUsageTokenCostInput(input UsageTokenCostInput) UsageTokenCostInput {
	input.InputTokens = maxInt64(input.InputTokens, 0)
	input.OutputTokens = maxInt64(input.OutputTokens, 0)
	input.CachedTokens = maxInt64(input.CachedTokens, 0)
	input.CacheReadTokens = maxInt64(input.CacheReadTokens, 0)
	input.CacheCreationTokens = maxInt64(input.CacheCreationTokens, 0)
	return input
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
