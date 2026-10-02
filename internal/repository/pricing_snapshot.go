package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/pricing"

	"gorm.io/gorm"
)

var ErrInvalidPricingSnapshot = errors.New("invalid pricing snapshot")

// LoadPricingSnapshot 从传入的 DB/transaction 一次加载并编译完整价格快照。
func LoadPricingSnapshot(ctx context.Context, db *gorm.DB) (*pricing.Snapshot, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	query := db.WithContext(ctx)
	settings, err := ListModelPriceSettings(query)
	if err != nil {
		return nil, err
	}
	rules, err := ListModelPriceRules(query)
	if err != nil {
		return nil, err
	}

	configIndexByID := make(map[int64]int, len(settings))
	configs := make([]pricing.ModelPricingConfig, len(settings))
	for index := range settings {
		setting := settings[index]
		multiplier := 1.0
		if setting.PriceMultiplier != nil {
			multiplier = *setting.PriceMultiplier
		}
		branches := make([]pricing.PriceBranch, 0)
		if err := json.Unmarshal([]byte(setting.BranchesJSON), &branches); err != nil {
			return nil, fmt.Errorf("%w: decode branches for model %q: %w", ErrInvalidPricingSnapshot, setting.Model, err)
		}
		configs[index] = pricing.ModelPricingConfig{
			Model: setting.Model, PricingStyle: setting.PricingStyle,
			BasePrices: pricing.BasePrices{
				Input: setting.PromptPricePer1M, Output: setting.CompletionPricePer1M,
				CacheRead: setting.CacheReadPricePer1M, CacheWrite: setting.CacheWritePricePer1M,
			},
			ModelMultiplier: multiplier, ConditionalMultipliers: make([]pricing.RuleConfig, 0), Branches: branches,
		}
		configIndexByID[settings[index].ID] = index
	}
	for index := range rules {
		configIndex, ok := configIndexByID[rules[index].ModelPriceSettingID]
		if !ok {
			return nil, fmt.Errorf("model price rule %d references missing price %d", rules[index].ID, rules[index].ModelPriceSettingID)
		}
		configs[configIndex].ConditionalMultipliers = append(configs[configIndex].ConditionalMultipliers, pricing.RuleConfig{
			Key:        rules[index].Key,
			Value:      rules[index].Value,
			Multiplier: rules[index].Multiplier,
		})
	}
	// config.Load 在应用启动加载价格前固定 time.Local；事务内编译与普通读取使用同一部署时区。
	snapshot, err := pricing.CompilePricingSnapshot(configs, time.Local)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPricingSnapshot, err)
	}
	return snapshot, nil
}
