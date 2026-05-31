package entities

import "time"

const (
	ModelPricingStyleOpenAI = "openai"
	ModelPricingStyleClaude = "claude"
)

// ModelPriceSetting 是模型价格配置实体，用于按模型计算成本。
type ModelPriceSetting struct {
	ID                      int64  `gorm:"primaryKey"`
	Model                   string `gorm:"uniqueIndex:uniq_model_price_settings_model"`
	PricingStyle            string `gorm:"not null;default:openai"`
	PromptPricePer1M        float64
	CompletionPricePer1M    float64
	CachePricePer1M         float64
	CacheCreationPricePer1M float64   `gorm:"not null;default:0"`
	CreatedAt               time.Time `gorm:"serializer:storageTime"`
	UpdatedAt               time.Time `gorm:"serializer:storageTime"`
}
