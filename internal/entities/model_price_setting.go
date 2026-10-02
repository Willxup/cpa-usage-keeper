package entities

import "time"

const (
	ModelPricingStyleOpenAI = "openai"
	ModelPricingStyleClaude = "claude"
)

// ModelPriceSetting 是模型价格配置实体，用于按模型计算成本。
type ModelPriceSetting struct {
	ID                   int64  `gorm:"primaryKey"`
	Model                string `gorm:"uniqueIndex:uniq_model_price_settings_model"`
	PricingStyle         string `gorm:"not null;default:openai"`
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CacheReadPricePer1M  float64
	CacheWritePricePer1M float64  `gorm:"column:cache_creation_price_per1_m;not null;default:0"`
	PriceMultiplier      *float64 `gorm:"not null;default:1"`
	// 条件分支在后续完整配置保存中写入；旧配置升级为明确的空数组。
	BranchesJSON string    `gorm:"column:branches_json;type:text;not null;default:'[]'"`
	CreatedAt    time.Time `gorm:"serializer:storageTime"`
	UpdatedAt    time.Time `gorm:"serializer:storageTime"`
}
