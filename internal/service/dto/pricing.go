package dto

// UpdatePricingInput 是更新定价的服务层输入。
type UpdatePricingInput struct {
	Model                   string
	PricingStyle            string
	PromptPricePer1M        float64
	CompletionPricePer1M    float64
	CachePricePer1M         float64
	CacheCreationPricePer1M float64
	PriceMultiplier         *float64
}

// PricingSyncPreview 是外部价格元数据同步前的预览结果。
type PricingSyncPreview struct {
	Source          string             `json:"source"`
	SourceURL       string             `json:"source_url"`
	MetadataModels  int                `json:"metadata_models"`
	Matches         []PricingSyncMatch `json:"matches"`
	UnmatchedModels []string           `json:"unmatched_models"`
}

// PricingSyncMatch 表示一个本地模型与外部元数据模型的匹配关系。
type PricingSyncMatch struct {
	Model                   string  `json:"model"`
	MatchedModel            string  `json:"matched_model"`
	MatchType               string  `json:"match_type"`
	SourceProviderID        string  `json:"source_provider_id"`
	SourceProviderName      string  `json:"source_provider_name"`
	PricingStyle            string  `json:"pricing_style"`
	PromptPricePer1M        float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M    float64 `json:"completion_price_per_1m"`
	CachePricePer1M         float64 `json:"cache_price_per_1m"`
	CacheCreationPricePer1M float64 `json:"cache_creation_price_per_1m"`
}
