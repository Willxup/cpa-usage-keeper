package entities

import "time"

// ModelPriceSetting 是模型价格配置实体，用于按模型计算成本。
type ModelPriceSetting struct {
	ID                   uint   `gorm:"primaryKey"`
	Model                string `gorm:"uniqueIndex:uniq_model_price_settings_model"`
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
	CreatedAt            time.Time `gorm:"serializer:storageTime"`
	UpdatedAt            time.Time `gorm:"serializer:storageTime"`
}
