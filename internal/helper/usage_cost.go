package helper

import "cpa-usage-keeper/internal/entities"

// UsageTokenCostInput 是价格计算的最小 token 输入，避免 repository 为事件和聚合行各维护一套公式。
type UsageTokenCostInput struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	Requests     int64
}

// UsageEventRequiresPricing 判断事件是否包含需要价格表解释的计费 token。
func UsageEventRequiresPricing(event entities.UsageEvent) bool {
	return UsageTokenInputRequiresPricing(UsageTokenCostInput{
		InputTokens:  event.InputTokens,
		OutputTokens: event.OutputTokens,
		CachedTokens: event.CachedTokens,
	})
}

// UsageTokenInputRequiresPricing 判断聚合 token 输入是否需要价格表才能给出完整 cost。
func UsageTokenInputRequiresPricing(input UsageTokenCostInput) bool {
	return input.InputTokens > 0 || input.OutputTokens > 0 || input.CachedTokens > 0
}

// CalculateUsageEventCost 复用通用 token 公式计算单条 usage_event 的费用。
func CalculateUsageEventCost(event entities.UsageEvent, pricing entities.ModelPriceSetting) float64 {
	return CalculateUsageTokenCost(UsageTokenCostInput{
		InputTokens:  event.InputTokens,
		OutputTokens: event.OutputTokens,
		CachedTokens: event.CachedTokens,
		Requests:     1,
	}, pricing)
}

// CalculateUsageTokenCost 按当前价格表计算费用，cached_tokens 单独计价后不再重复计入 prompt。
func CalculateUsageTokenCost(input UsageTokenCostInput, pricing entities.ModelPriceSetting) float64 {
	inputTokens := input.InputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	outputTokens := input.OutputTokens
	if outputTokens < 0 {
		outputTokens = 0
	}
	cachedTokens := input.CachedTokens
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	requests := input.Requests
	if requests < 0 {
		requests = 0
	}
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(outputTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTokens)/1_000_000.0)*pricing.CachePricePer1M +
		float64(requests)*pricing.PricePerRequest
}
