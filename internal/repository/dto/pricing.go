package dto

// ModelPriceSettingInput 是价格设置写入参数。
type ModelPriceSettingInput struct {
	Model                   string
	PricingStyle            string
	PromptPricePer1M        float64
	CompletionPricePer1M    float64
	CachePricePer1M         float64
	CacheCreationPricePer1M float64
	PriceMultiplier         *float64
}
