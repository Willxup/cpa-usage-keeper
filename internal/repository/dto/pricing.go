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
	// nil 表示旧基础价写入保留分支；完整配置保存传入非 nil 的 JSON 数组。
	BranchesJSON *string
}
