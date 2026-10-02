package entities

// PricingState 只保存已提交价格配置的修订号；费用回写不改变该行。
type PricingState struct {
	ID             int64 `gorm:"primaryKey;autoIncrement:false;check:chk_pricing_state_singleton,id = 1"`
	ConfigRevision int64 `gorm:"not null;default:0;check:chk_pricing_state_revision,config_revision >= 0"`
}

func (PricingState) TableName() string { return "pricing_state" }
