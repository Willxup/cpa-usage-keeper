package repository

import (
	"context"
	"errors"
	"fmt"

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
	configs := make([]pricing.ModelConfig, len(settings))
	for index := range settings {
		configs[index].Pricing = settings[index]
		configIndexByID[settings[index].ID] = index
	}
	for index := range rules {
		configIndex, ok := configIndexByID[rules[index].ModelPriceSettingID]
		if !ok {
			return nil, fmt.Errorf("model price rule %d references missing price %d", rules[index].ID, rules[index].ModelPriceSettingID)
		}
		configs[configIndex].Rules = append(configs[configIndex].Rules, pricing.RuleConfig{
			Key:        rules[index].Key,
			Value:      rules[index].Value,
			Multiplier: rules[index].Multiplier,
		})
	}
	snapshot, err := pricing.CompileSnapshot(configs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPricingSnapshot, err)
	}
	return snapshot, nil
}
