package helper

import "cpa-usage-keeper/internal/entities"

// UsageTokenCostInput 是归一化事件的四类 Token 输入，供落库及显式费用回写使用。
type UsageTokenCostInput struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// UsageTokenInputRequiresPricing 判断事件 token 输入是否需要价格表才能给出完整 cost。
func UsageTokenInputRequiresPricing(input UsageTokenCostInput) bool {
	return input.InputTokens > 0 || input.OutputTokens > 0 || input.CacheReadTokens > 0 || input.CacheCreationTokens > 0
}

// CalculateUsageTokenCost 按四类 Token 单价计算事件总费用（USD），再应用模型倍率。
// 输入已经归一化；这里只沿用负数截零及缓存扣除，不重新合并 Claude 输入。
// 返回未按展示精度舍入的 float64；配置有限值检查由保存入口负责，零倍率直接返回免费。
func CalculateUsageTokenCost(input UsageTokenCostInput, pricing entities.ModelPriceSetting) float64 {
	input = clampUsageTokenCostInput(input)
	multiplier := modelPriceMultiplier(pricing)
	if multiplier == 0 {
		return 0
	}
	// 逐步扣除避免异常大缓存值相加导致 int64 下溢，保留原普通输入截零语义。
	normalInputTokens := input.InputTokens
	normalInputTokens -= min(normalInputTokens, input.CacheReadTokens)
	normalInputTokens -= min(normalInputTokens, input.CacheCreationTokens)
	inputCost := (float64(normalInputTokens) / 1_000_000.0) * pricing.PromptPricePer1M
	cacheReadCost := (float64(input.CacheReadTokens) / 1_000_000.0) * pricing.CacheReadPricePer1M
	cacheWriteCost := (float64(input.CacheCreationTokens) / 1_000_000.0) * pricing.CacheWritePricePer1M
	outputCost := (float64(input.OutputTokens) / 1_000_000.0) * pricing.CompletionPricePer1M
	return (inputCost + cacheReadCost + cacheWriteCost + outputCost) * multiplier
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

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
