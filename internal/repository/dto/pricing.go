package dto

// ModelPriceSettingInput 是价格设置写入参数。
type ModelPriceSettingInput struct {
	Model                string
	PricingStyle         string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CacheReadPricePer1M  float64
	CacheWritePricePer1M float64
	PriceMultiplier      *float64
}
