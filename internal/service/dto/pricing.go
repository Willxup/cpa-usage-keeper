package dto

// UpdatePricingInput 是更新定价的服务层输入。
type UpdatePricingInput struct {
	Model                string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
	PricePerRequest      float64
}
