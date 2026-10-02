package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

// ListPricingModels 在短配置锁内读取同一轮已提交快照和修订号。
// 不查询当前事件费用，也不阻塞已持有旧 Resolver 的事件批次。
func (s *pricingService) ListPricingModels(ctx context.Context) (servicedto.PricingModelsResponse, error) {
	if s == nil || s.db == nil {
		return servicedto.PricingModelsResponse{}, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	revision, err := repository.LoadPricingConfigRevision(s.db.WithContext(ctx))
	if err != nil {
		return servicedto.PricingModelsResponse{}, err
	}
	return servicedto.PricingModelsResponse{Models: s.catalog.Snapshot().PricingModelConfigs(), ConfigRevision: revision}, nil
}

// SavePricingModel 保存一个模型的全部基础价、分支及条件倍率；最后成功提交的完整配置生效。
// 先校验单模型合同，再在 mutationMu 下同事务替换配置/规则、编译全快照并递增修订；
// 任一步失败均回滚，只有 COMMIT 成功才发布快照，旧 Resolver 继续按旧价格计算。
func (s *pricingService) SavePricingModel(ctx context.Context, input pricing.ModelPricingConfig) (servicedto.SavePricingModelResponse, error) {
	// 全量保存必须保留默认价作为兜底；读取已有配置不加此限制，避免阻断旧数据启动。
	for index, branch := range input.Branches {
		if (branch.Days == "" || branch.Days == pricing.DaysAll) && branch.Context.Type == pricing.ContextAll && branch.Period.Type == pricing.PeriodAll {
			return servicedto.SavePricingModelResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingInput, &pricing.ValidationError{
				Path: fmt.Sprintf("branches[%d].context", index), Code: "default_conflict", Reason: "an unconditional branch replaces the default price",
			})
		}
	}
	validated, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{input}, time.Local)
	if err != nil {
		return servicedto.SavePricingModelResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingInput, err)
	}
	config, ok := validated.PricingModelConfig(input.Model)
	if !ok {
		return servicedto.SavePricingModelResponse{}, fmt.Errorf("%w: normalized model is missing", ErrInvalidPricingInput)
	}
	branchesJSON, err := json.Marshal(config.Branches)
	if err != nil {
		return servicedto.SavePricingModelResponse{}, fmt.Errorf("encode pricing branches: %w", err)
	}
	branchesValue := string(branchesJSON)
	multiplier := config.ModelMultiplier
	_, revision, err := s.mutatePricingRevision(ctx, func(tx *gorm.DB) error {
		_, err := repository.UpsertModelPriceSetting(tx, repodto.ModelPriceSettingInput{
			Model: config.Model, PricingStyle: config.PricingStyle,
			PromptPricePer1M: config.BasePrices.Input, CompletionPricePer1M: config.BasePrices.Output,
			CacheReadPricePer1M: config.BasePrices.CacheRead, CacheWritePricePer1M: config.BasePrices.CacheWrite,
			PriceMultiplier: &multiplier, BranchesJSON: &branchesValue,
		})
		if err != nil {
			return err
		}
		rules := make([]repodto.ModelPriceRuleInput, len(config.ConditionalMultipliers))
		for index, rule := range config.ConditionalMultipliers {
			rules[index] = repodto.ModelPriceRuleInput{Key: rule.Key, Value: rule.Value, Multiplier: rule.Multiplier}
		}
		_, err = repository.ReplaceModelPriceRules(tx, config.Model, rules)
		return err
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return servicedto.SavePricingModelResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingInput, err)
		}
		return servicedto.SavePricingModelResponse{}, err
	}
	return servicedto.SavePricingModelResponse{Model: config.Model, ConfigRevision: revision}, nil
}

// DeletePricingModel 同事务删除当前模型配置及级联规则并推进修订；已存事件费用不变。
// 不存在的模型不产生新修订，成功 COMMIT 后才发布不含该模型的快照。
func (s *pricingService) DeletePricingModel(ctx context.Context, model string) (servicedto.DeletePricingModelResponse, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return servicedto.DeletePricingModelResponse{}, fmt.Errorf("%w: model is required", ErrInvalidPricingInput)
	}
	_, revision, err := s.mutatePricingRevision(ctx, func(tx *gorm.DB) error {
		return repository.DeleteModelPriceSettingRequired(tx, model)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return servicedto.DeletePricingModelResponse{}, fmt.Errorf("%w: %s", ErrPricingModelNotFound, model)
	}
	if err != nil {
		return servicedto.DeletePricingModelResponse{}, err
	}
	return servicedto.DeletePricingModelResponse{ConfigRevision: revision}, nil
}
