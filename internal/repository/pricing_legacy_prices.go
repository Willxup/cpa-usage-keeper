package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/pricing"
	"gorm.io/gorm"
)

// publishedPricingSetting 冻结M2完成后的旧价格列；不得关联当前实体而提前读取branches_json。
type publishedPricingSetting struct {
	ID                   int64
	Model                string
	PricingStyle         string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CacheReadPricePer1M  float64
	CacheWritePricePer1M float64 `gorm:"column:cache_creation_price_per1_m"`
	PriceMultiplier      *float64
}

// publishedPricingRule 只读取旧规则的业务列，避免实体关联触及未来模型结构。
type publishedPricingRule struct {
	ID                  int64
	ModelPriceSettingID int64
	Key                 string
	Value               string
	Multiplier          float64
}

// LoadPublishedPricingConfigs 在旧迁移完成、费用结构增列前读取等价价格基线。
// 四项单价单位为USD／1M Token；保留旧模型／条件倍率，明确零倍率不改成1，旧NULL沿用原默认1。
// 只查询旧物理列并编译校验，不写库、不重新归一化Token；调用方需已停止价格写入并持久固定返回值。
func LoadPublishedPricingConfigs(ctx context.Context, db *gorm.DB) ([]pricing.ModelPricingConfig, error) {
	if db == nil {
		return nil, fmt.Errorf("published pricing database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	query := db.WithContext(ctx)
	var settings []publishedPricingSetting
	if err := query.Table("model_price_settings").Select([]string{
		"id", "model", "pricing_style", "prompt_price_per1_m", "completion_price_per1_m",
		"cache_read_price_per1_m", "cache_creation_price_per1_m", "price_multiplier",
	}).Order("id ASC").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("load published model prices: %w", err)
	}
	var rules []publishedPricingRule
	if err := query.Table("model_price_rules").Select([]string{
		"id", "model_price_setting_id", "key", "value", "multiplier",
	}).Order("id ASC").Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("load published price rules: %w", err)
	}
	configs := make([]pricing.ModelPricingConfig, len(settings))
	byID := make(map[int64]int, len(settings))
	for index, setting := range settings {
		multiplier := 1.0
		if setting.PriceMultiplier != nil {
			multiplier = *setting.PriceMultiplier
		}
		configs[index] = pricing.ModelPricingConfig{
			Model: setting.Model, PricingStyle: setting.PricingStyle,
			BasePrices: pricing.BasePrices{
				Input: setting.PromptPricePer1M, Output: setting.CompletionPricePer1M,
				CacheRead: setting.CacheReadPricePer1M, CacheWrite: setting.CacheWritePricePer1M,
			},
			ModelMultiplier:        multiplier,
			ConditionalMultipliers: make([]pricing.RuleConfig, 0),
			Branches:               make([]pricing.PriceBranch, 0),
		}
		byID[setting.ID] = index
	}
	for _, rule := range rules {
		index, exists := byID[rule.ModelPriceSettingID]
		if !exists {
			return nil, fmt.Errorf("published price rule %d references missing price %d", rule.ID, rule.ModelPriceSettingID)
		}
		configs[index].ConditionalMultipliers = append(configs[index].ConditionalMultipliers, pricing.RuleConfig{
			Key: rule.Key, Value: rule.Value, Multiplier: rule.Multiplier,
		})
	}
	snapshot, err := pricing.CompilePricingSnapshot(configs, time.Local)
	if err != nil {
		return nil, fmt.Errorf("%w: published pricing baseline: %w", ErrInvalidPricingSnapshot, err)
	}
	return snapshot.PricingModelConfigs(), nil
}
