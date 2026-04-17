package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]models.ModelPriceSetting, error)
	UpdatePricing(context.Context, UpdatePricingInput) (*models.ModelPriceSetting, error)
}

type UpdatePricingInput struct {
	Model                string
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
}

type pricingService struct {
	db *gorm.DB
}

func NewPricingService(db *gorm.DB) PricingProvider {
	return &pricingService{db: db}
}

func (s *pricingService) ListUsedModels(context.Context) ([]string, error) {
	return repository.ListUsedModels(s.db)
}

func (s *pricingService) ListPricing(context.Context) ([]models.ModelPriceSetting, error) {
	return repository.ListModelPriceSettings(s.db)
}

func (s *pricingService) UpdatePricing(_ context.Context, input UpdatePricingInput) (*models.ModelPriceSetting, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CachePricePer1M < 0 {
		return nil, fmt.Errorf("prices must be non-negative")
	}

	usedModels, err := repository.ListUsedModels(s.db)
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

	return repository.UpsertModelPriceSetting(s.db, repository.ModelPriceSettingInput{
		Model:                modelName,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CachePricePer1M:      input.CachePricePer1M,
	})
}
