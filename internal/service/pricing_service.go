package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]entities.ModelPriceSetting, error)
	PreviewPricingSync(context.Context) (servicedto.PricingSyncPreview, error)
	UpdatePricing(context.Context, servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error)
	DeletePricing(context.Context, string) error
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db            *gorm.DB
	modelsFetcher ModelsFetcher
}

func NewPricingService(db *gorm.DB, modelsFetcher ...ModelsFetcher) PricingProvider {
	service := &pricingService{db: db}
	if len(modelsFetcher) > 0 {
		service.modelsFetcher = modelsFetcher[0]
	}
	return service
}

func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return s.effectiveModels(ctx)
}

func (s *pricingService) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	return repository.ListModelPriceSettings(s.db)
}

func (s *pricingService) UpdatePricing(ctx context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	pricingStyle := strings.ToLower(strings.TrimSpace(input.PricingStyle))
	if pricingStyle == "" {
		pricingStyle = entities.ModelPricingStyleOpenAI
	}
	if pricingStyle != entities.ModelPricingStyleOpenAI && pricingStyle != entities.ModelPricingStyleClaude {
		return nil, fmt.Errorf("pricing_style must be openai or claude")
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CachePricePer1M < 0 || input.CacheCreationPricePer1M < 0 {
		return nil, fmt.Errorf("prices must be non-negative")
	}
	if input.PriceMultiplier != nil {
		multiplier := *input.PriceMultiplier
		if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			return nil, fmt.Errorf("price_multiplier must be non-negative")
		}
	}

	return repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
		Model:                   modelName,
		PricingStyle:            pricingStyle,
		PromptPricePer1M:        input.PromptPricePer1M,
		CompletionPricePer1M:    input.CompletionPricePer1M,
		CachePricePer1M:         input.CachePricePer1M,
		CacheCreationPricePer1M: input.CacheCreationPricePer1M,
		PriceMultiplier:         input.PriceMultiplier,
	})
}

func (s *pricingService) DeletePricing(_ context.Context, model string) error {
	return repository.DeleteModelPriceSetting(s.db, model)
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
