package dto

// UpdatePricingInput 是更新定价的服务层输入。
type UpdatePricingInput struct {
	Model                string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
}

// UsedModelOption 是前端定价表单的结构化模型候选项。
type UsedModelOption struct {
	Value  string
	Source string
	Model  string
}
