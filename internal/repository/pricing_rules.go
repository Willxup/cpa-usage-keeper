package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var modelPriceRuleColumns = []string{
	"id",
	"model_price_setting_id",
	"key",
	"value",
	"multiplier",
	"created_at",
	"updated_at",
}

// modelPriceRuleInsert 只用于写入已验证的完整规则；去掉实体的 default:1 标签，
// 让合法倍率 0 作为明确列值插入，同时继续由 GORM 填充 ID 与时间戳。
type modelPriceRuleInsert struct {
	ID                  int64 `gorm:"primaryKey"`
	ModelPriceSettingID int64
	Key                 string
	Value               string
	Multiplier          float64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime"`
}

func (modelPriceRuleInsert) TableName() string { return "model_price_rules" }

func ListModelPriceRules(db *gorm.DB) ([]entities.ModelPriceRule, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	rules := make([]entities.ModelPriceRule, 0)
	if err := db.Select(modelPriceRuleColumns).
		Order("model_price_setting_id asc").
		Order("id asc").
		Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("list model price rules: %w", err)
	}
	return rules, nil
}

// ReplaceModelPriceRules 在调用方事务内整体替换一个模型的规则集合。
func ReplaceModelPriceRules(db *gorm.DB, model string, inputs []dto.ModelPriceRuleInput) ([]entities.ModelPriceRule, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	var setting entities.ModelPriceSetting
	if err := db.Clauses(dbresolver.Write).
		Select("id", "model").
		Where("model = ?", model).
		Take(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("model price %q not found: %w", model, err)
		}
		return nil, fmt.Errorf("load model price %q: %w", model, err)
	}
	if err := db.Where("model_price_setting_id = ?", setting.ID).Delete(&entities.ModelPriceRule{}).Error; err != nil {
		return nil, fmt.Errorf("delete model price rules for %q: %w", model, err)
	}

	inserts := make([]modelPriceRuleInsert, len(inputs))
	for index := range inputs {
		inserts[index] = modelPriceRuleInsert{
			ModelPriceSettingID: setting.ID,
			Key:                 inputs[index].Key,
			Value:               inputs[index].Value,
			Multiplier:          inputs[index].Multiplier,
		}
	}
	if len(inserts) == 0 {
		return []entities.ModelPriceRule{}, nil
	}
	if err := db.CreateInBatches(&inserts, insertBatchSize(entities.ModelPriceRule{})).Error; err != nil {
		return nil, fmt.Errorf("create model price rules for %q: %w", model, err)
	}
	rules := make([]entities.ModelPriceRule, len(inserts))
	for index, inserted := range inserts {
		rules[index] = entities.ModelPriceRule{
			ID: inserted.ID, ModelPriceSettingID: inserted.ModelPriceSettingID,
			Key: inserted.Key, Value: inserted.Value, Multiplier: inserted.Multiplier,
			CreatedAt: inserted.CreatedAt, UpdatedAt: inserted.UpdatedAt,
		}
	}
	return rules, nil
}
