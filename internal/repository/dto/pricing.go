package dto

// ModelPriceSettingInput 是价格设置写入参数。
type ModelPriceSettingInput struct {
	Model                string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
}

// UsedModelOption 是定价页面可选择的模型项；Value 是最终保存的 pricing key。
type UsedModelOption struct {
	Value  string
	Source string
	Model  string
}
