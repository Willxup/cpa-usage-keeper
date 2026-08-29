package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var ErrInvalidPricingInput = errors.New("invalid pricing input")

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]entities.ModelPriceSetting, error)
	PreviewPricingSync(context.Context) (servicedto.PricingSyncPreview, error)
	UpdatePricing(context.Context, servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error)
	UpdatePricingBatch(context.Context, []servicedto.UpdatePricingInput) ([]entities.ModelPriceSetting, error)
	DeletePricing(context.Context, string) error
	ListPricingRules(context.Context, string) ([]servicedto.PricingRule, error)
	ReplacePricingRules(context.Context, servicedto.ReplacePricingRulesInput) ([]servicedto.PricingRule, error)
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db            *gorm.DB
	modelsFetcher ModelsFetcher
	catalog       *pricing.Catalog
	mutationMu    sync.Mutex
}

func NewPricingService(db *gorm.DB, catalog *pricing.Catalog, modelsFetcher ...ModelsFetcher) PricingProvider {
	service := &pricingService{db: db, catalog: requirePricingCatalog(catalog)}
	if len(modelsFetcher) > 0 {
		service.modelsFetcher = modelsFetcher[0]
	}
	return service
}

func requirePricingCatalog(catalog *pricing.Catalog) *pricing.Catalog {
	if catalog == nil {
		panic("pricing catalog is required")
	}
	return catalog
}

func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return s.effectiveModels(ctx)
}

func (s *pricingService) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	configs := s.catalog.Snapshot().ModelConfigs()
	settings := make([]entities.ModelPriceSetting, len(configs))
	for index := range configs {
		settings[index] = configs[index].Pricing
	}
	return settings, nil
}

func (s *pricingService) UpdatePricing(ctx context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	settings, err := s.UpdatePricingBatch(ctx, []servicedto.UpdatePricingInput{input})
	if err != nil {
		return nil, err
	}
	return &settings[0], nil
}

func (s *pricingService) UpdatePricingBatch(ctx context.Context, inputs []servicedto.UpdatePricingInput) ([]entities.ModelPriceSetting, error) {
	if len(inputs) == 0 {
		return []entities.ModelPriceSetting{}, nil
	}
	normalized := make([]repodto.ModelPriceSettingInput, len(inputs))
	seenModels := make(map[string]struct{}, len(inputs))
	for index := range inputs {
		input, err := normalizePricingInput(inputs[index])
		if err != nil {
			return nil, fmt.Errorf("pricing at index %d: %w", index, err)
		}
		if _, exists := seenModels[input.Model]; exists {
			return nil, fmt.Errorf("pricing at index %d: %w: duplicate model %q", index, ErrInvalidPricingInput, input.Model)
		}
		seenModels[input.Model] = struct{}{}
		normalized[index] = input
	}

	settings := make([]entities.ModelPriceSetting, len(normalized))
	_, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		for index := range normalized {
			setting, mutationErr := repository.UpsertModelPriceSetting(tx, normalized[index])
			if mutationErr != nil {
				return mutationErr
			}
			settings[index] = *setting
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPricingInput, err)
		}
		return nil, err
	}
	return settings, nil
}

func normalizePricingInput(input servicedto.UpdatePricingInput) (repodto.ModelPriceSettingInput, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: model is required", ErrInvalidPricingInput)
	}
	pricingStyle := strings.ToLower(strings.TrimSpace(input.PricingStyle))
	if pricingStyle == "" {
		pricingStyle = entities.ModelPricingStyleOpenAI
	}
	if pricingStyle != entities.ModelPricingStyleOpenAI && pricingStyle != entities.ModelPricingStyleClaude {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: pricing_style must be openai or claude", ErrInvalidPricingInput)
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CacheReadPricePer1M < 0 || input.CacheWritePricePer1M < 0 {
		return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: prices must be non-negative", ErrInvalidPricingInput)
	}
	if input.PriceMultiplier != nil {
		multiplier := *input.PriceMultiplier
		if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return repodto.ModelPriceSettingInput{}, fmt.Errorf("%w: price_multiplier must be non-negative", ErrInvalidPricingInput)
		}
	}
	return repodto.ModelPriceSettingInput{
		Model:                modelName,
		PricingStyle:         pricingStyle,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CacheReadPricePer1M:  input.CacheReadPricePer1M,
		CacheWritePricePer1M: input.CacheWritePricePer1M,
		PriceMultiplier:      input.PriceMultiplier,
	}, nil
}

func (s *pricingService) DeletePricing(ctx context.Context, model string) error {
	_, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		return repository.DeleteModelPriceSetting(tx, model)
	})
	return err
}

func (s *pricingService) effectiveModels(ctx context.Context) ([]string, error) {
	localModels, err := repository.ListUsedModels(s.db)
	if err != nil {
		return nil, err
	}
	if s.modelsFetcher == nil {
		return localModels, nil
	}

	result, err := s.modelsFetcher.FetchModels(ctx)
	if err != nil {
		logrus.WithError(err).Error("pricing model listing falling back to local usage aggregation")
		return localModels, nil
	}

	logrus.Debug("pricing model listing using CPA models endpoint")
	return mergeModelNames(localModels, extractCPAModelIDs(result)), nil
}

func extractCPAModelIDs(result *response.ModelsResult) []string {
	if result == nil {
		return []string{}
	}
	models := make([]string, 0, len(result.Payload.Data))
	for _, model := range result.Payload.Data {
		models = append(models, model.ID)
	}
	return models
}

func mergeModelNames(modelLists ...[]string) []string {
	total := 0
	for _, list := range modelLists {
		total += len(list)
	}
	seen := make(map[string]struct{}, total)
	models := make([]string, 0, total)
	for _, list := range modelLists {
		for _, model := range list {
			id := strings.TrimSpace(model)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models
}
