package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var ErrInvalidPricingSyncSource = errors.New("invalid pricing sync source")

func validPricingSyncSource(source string) bool {
	return source == "models-dev" || source == "litellm"
}

// FetchPricingSync 拉取选定来源并沿用现有模型匹配优先级，只返回审核所需的基础价。
func (s *pricingService) FetchPricingSync(ctx context.Context, source string) (servicedto.PricingSyncFetchResponse, error) {
	if !validPricingSyncSource(source) {
		return servicedto.PricingSyncFetchResponse{}, ErrInvalidPricingSyncSource
	}
	models, catalog, err := s.loadPricingSyncCatalog(ctx, source)
	if err != nil {
		return servicedto.PricingSyncFetchResponse{}, err
	}
	matches, unmatched := collectPricingSyncMatches(models, catalog)
	response := servicedto.PricingSyncFetchResponse{
		Source: source, Matches: make([]servicedto.PricingSyncFetchMatch, 0, len(matches)), UnmatchedModels: unmatched,
	}
	for _, match := range matches {
		response.Matches = append(response.Matches, servicedto.PricingSyncFetchMatch{
			Model: match.Model, MatchedModel: match.MatchedModel, Provider: match.SourceProviderName,
			PricingStyle: match.PricingStyle, BasePrices: match.BasePrices,
		})
	}
	return response, nil
}

// ApplyPricingSync 只把审核后的四项基础价写入当时数据库配置；新模型才初始化风格、倍率和空规则。
// 所有项、候选编译与单次修订在同一事务内完成，提交后只发布一次快照，不改历史费用。
func (s *pricingService) ApplyPricingSync(ctx context.Context, request servicedto.PricingSyncApplyRequest) (servicedto.PricingSyncApplyResponse, error) {
	if !validPricingSyncSource(request.Source) {
		return servicedto.PricingSyncApplyResponse{}, ErrInvalidPricingSyncSource
	}
	if len(request.Items) == 0 {
		return servicedto.PricingSyncApplyResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingInput,
			&pricing.ValidationError{Path: "items", Code: "invalid", Reason: "items must not be empty"})
	}
	names := make([]string, len(request.Items))
	seen := make(map[string]struct{}, len(request.Items))
	for index, item := range request.Items {
		name := strings.TrimSpace(item.Model)
		if name == "" {
			return servicedto.PricingSyncApplyResponse{}, pricingSyncItemInputError(index,
				&pricing.ValidationError{Path: "model", Code: "required", Reason: "model is required"})
		}
		if _, exists := seen[name]; exists {
			return servicedto.PricingSyncApplyResponse{}, pricingSyncItemInputError(index,
				&pricing.ValidationError{Path: "model", Code: "invalid", Reason: "duplicate model"})
		}
		seen[name] = struct{}{}
		names[index] = name
	}
	candidate, revision, err := s.mutatePricingRevision(ctx, func(tx *gorm.DB) error {
		// 事务内读取当前真实配置；拉取后的普通保存即使先提交，也不会被旧审核草稿覆盖。
		current, err := repository.LoadPricingSnapshot(ctx, tx)
		if err != nil {
			return err
		}
		for index, item := range request.Items {
			name := names[index]
			config, exists := current.PricingModelConfig(name)
			if !exists {
				config = pricing.ModelPricingConfig{Model: name, ModelMultiplier: 1,
					ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{}}
			}
			config.BasePrices = item.BasePrices
			// 来源风格必须合法，但已有模型的风格仍以事务内旧值为准。
			config.PricingStyle = item.PricingStyle
			if _, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{config}, time.Local); err != nil {
				return pricingSyncItemInputError(index, err)
			}
			if !exists {
				one, emptyBranches := 1.0, "[]"
				_, err := repository.UpsertModelPriceSetting(tx, repodto.ModelPriceSettingInput{
					Model: name, PricingStyle: item.PricingStyle,
					PromptPricePer1M: item.BasePrices.Input, CompletionPricePer1M: item.BasePrices.Output,
					CacheReadPricePer1M: item.BasePrices.CacheRead, CacheWritePricePer1M: item.BasePrices.CacheWrite,
					PriceMultiplier: &one, BranchesJSON: &emptyBranches,
				})
				if err != nil {
					return err
				}
				continue
			}
			if err := tx.Clauses(dbresolver.Write).Model(&entities.ModelPriceSetting{}).Where("model = ?", name).Updates(map[string]any{
				"prompt_price_per1_m": item.BasePrices.Input, "completion_price_per1_m": item.BasePrices.Output,
				"cache_read_price_per1_m": item.BasePrices.CacheRead, "cache_creation_price_per1_m": item.BasePrices.CacheWrite,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return servicedto.PricingSyncApplyResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingInput, err)
		}
		return servicedto.PricingSyncApplyResponse{}, err
	}
	response := servicedto.PricingSyncApplyResponse{Models: make([]pricing.ModelPricingConfig, 0, len(names)), ConfigRevision: revision}
	for _, name := range names {
		config, ok := candidate.PricingModelConfig(name)
		if !ok {
			return servicedto.PricingSyncApplyResponse{}, fmt.Errorf("applied model missing from committed snapshot")
		}
		response.Models = append(response.Models, config)
	}
	return response, nil
}

func pricingSyncItemInputError(index int, err error) error {
	var field *pricing.ValidationError
	if errors.As(err, &field) {
		return fmt.Errorf("%w: %w", ErrInvalidPricingInput, &pricing.ValidationError{
			Path: fmt.Sprintf("items[%d].%s", index, field.Path), Code: field.Code, Reason: field.Reason,
		})
	}
	return fmt.Errorf("%w: item %d: %w", ErrInvalidPricingInput, index, err)
}
