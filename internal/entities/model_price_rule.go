package entities

import "time"

// ModelPriceRule 是隶属于一条模型价格的精确字段倍率规则。
type ModelPriceRule struct {
	ID                  int64             `gorm:"primaryKey"`
	ModelPriceSettingID int64             `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:1"`
	ModelPriceSetting   ModelPriceSetting `gorm:"foreignKey:ModelPriceSettingID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Key                 string            `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:2"`
	Value               string            `gorm:"not null;uniqueIndex:uniq_model_price_rules_identity,priority:3"`
	Multiplier          float64           `gorm:"not null;default:1"`
	CreatedAt           time.Time         `gorm:"serializer:storageTime"`
	UpdatedAt           time.Time         `gorm:"serializer:storageTime"`
}
