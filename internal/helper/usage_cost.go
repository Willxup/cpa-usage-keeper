package helper

import "cpa-usage-keeper/internal/entities"

// UsageTokenCostInput 是价格计算的最小 token 输入，避免 repository 为事件和聚合行各维护一套公式。
type UsageTokenCostInput struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

type UsageTokenCostBreakdown struct {
	UncachedInputCostUSD float64
	CacheReadCostUSD     float64
	CacheWriteCostUSD    float64
	OutputCostUSD        float64
	TotalCostUSD         float64
}

// UsageTokenInputRequiresPricing 判断聚合 token 输入是否需要价格表才能给出完整 cost。
func UsageTokenInputRequiresPricing(input UsageTokenCostInput) bool {
	return input.InputTokens > 0 || input.OutputTokens > 0 || input.CacheReadTokens > 0 || input.CacheCreationTokens > 0
}

// CalculateUsageTokenCostBreakdown 按普通输入、缓存读取、缓存写入和输出四段独立计价。
func CalculateUsageTokenCostBreakdown(input UsageTokenCostInput, pricing entities.ModelPriceSetting) UsageTokenCostBreakdown {
	input = clampUsageTokenCostInput(input)
	breakdown := calculateUsageTokenCostBreakdown(input, pricing)
	return scaleUsageTokenCostBreakdown(breakdown, modelPriceMultiplier(pricing))
}

func calculateUsageTokenCostBreakdown(input UsageTokenCostInput, pricing entities.ModelPriceSetting) UsageTokenCostBreakdown {
	normalInputTokens := input.InputTokens - input.CacheReadTokens - input.CacheCreationTokens
	if normalInputTokens < 0 {
		normalInputTokens = 0
	}
	breakdown := UsageTokenCostBreakdown{
		UncachedInputCostUSD: (float64(normalInputTokens) / 1_000_000.0) * pricing.PromptPricePer1M,
		CacheReadCostUSD:     (float64(input.CacheReadTokens) / 1_000_000.0) * pricing.CacheReadPricePer1M,
		CacheWriteCostUSD:    (float64(input.CacheCreationTokens) / 1_000_000.0) * pricing.CacheWritePricePer1M,
		OutputCostUSD:        (float64(input.OutputTokens) / 1_000_000.0) * pricing.CompletionPricePer1M,
	}
	breakdown.TotalCostUSD = breakdown.UncachedInputCostUSD + breakdown.CacheReadCostUSD + breakdown.CacheWriteCostUSD + breakdown.OutputCostUSD
	return breakdown
}

func clampUsageTokenCostInput(input UsageTokenCostInput) UsageTokenCostInput {
	input.InputTokens = maxInt64(input.InputTokens, 0)
	input.OutputTokens = maxInt64(input.OutputTokens, 0)
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
	breakdown.UncachedInputCostUSD *= multiplier
	breakdown.CacheReadCostUSD *= multiplier
	breakdown.CacheWriteCostUSD *= multiplier
	breakdown.OutputCostUSD *= multiplier
	breakdown.TotalCostUSD *= multiplier
	return breakdown
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
