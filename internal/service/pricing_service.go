package service

import (
	"context"
	"fmt"
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
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CachePricePer1M < 0 || input.PricePerRequest < 0 {
		return nil, fmt.Errorf("prices must be non-negative")
	}

	usedModels, err := s.effectiveModels(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[string]struct{}, len(usedModels))
	for _, model := range usedModels {
		index[model] = struct{}{}
	}
	if _, ok := index[modelName]; !ok {
		sort.Strings(usedModels)
		return nil, fmt.Errorf("model %q has not been used", modelName)
	}

	return repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
		Model:                modelName,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CachePricePer1M:      input.CachePricePer1M,
		PricePerRequest:      input.PricePerRequest,
	})
}

func (s *pricingService) DeletePricing(_ context.Context, model string) error {
	return repository.DeleteModelPriceSetting(s.db, model)
}

func (s *pricingService) effectiveModels(ctx context.Context) ([]string, error) {
	if s.modelsFetcher == nil {
		return repository.ListUsedModels(s.db)
	}

	result, err := s.modelsFetcher.FetchModels(ctx)
	if err != nil {
		logrus.WithError(err).Error("pricing model listing falling back to local usage aggregation")
		return repository.ListUsedModels(s.db)
	}

	logrus.Debug("pricing model listing using CPA models endpoint")
	return normalizeCPAModels(result), nil
}

func normalizeCPAModels(result *response.ModelsResult) []string {
	if result == nil {
		return []string{}
	}
	seen := make(map[string]struct{}, len(result.Payload.Data))
	models := make([]string, 0, len(result.Payload.Data))
	for _, model := range result.Payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models
}
