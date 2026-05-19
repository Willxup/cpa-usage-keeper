package service

import (
	"context"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type PricingProvider interface {
	ListUsedModels(context.Context) ([]string, error)
	ListPricing(context.Context) ([]entities.ModelPriceSetting, error)
	UpdatePricing(context.Context, servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error)
	DeletePricing(context.Context, string) error
}

type pricingService struct {
	db *gorm.DB
}

// NewPricingService avoids CPA /v1/models for pricing because that endpoint exposes public aliases.
// Prices are keyed by usage_events.model, the upstream request model used by cost calculation.
func NewPricingService(db *gorm.DB) PricingProvider {
	return &pricingService{db: db}
}

func (s *pricingService) ListUsedModels(ctx context.Context) ([]string, error) {
	return repository.ListUsedModels(s.db.WithContext(ctx))
}

func (s *pricingService) ListPricing(ctx context.Context) ([]entities.ModelPriceSetting, error) {
	return repository.ListModelPriceSettings(s.db.WithContext(ctx))
}

func (s *pricingService) UpdatePricing(ctx context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	modelName := strings.TrimSpace(input.Model)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	if input.PromptPricePer1M < 0 || input.CompletionPricePer1M < 0 || input.CachePricePer1M < 0 {
		return nil, fmt.Errorf("prices must be non-negative")
	}

	db := s.db.WithContext(ctx)
	usedModels, err := repository.ListUsedModels(db)
	if err != nil {
		return nil, err
	}
	index := make(map[string]struct{}, len(usedModels))
	for _, model := range usedModels {
		index[model] = struct{}{}
	}
	if _, ok := index[modelName]; !ok {
		return nil, fmt.Errorf("model %q has not been used", modelName)
	}

	return repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                modelName,
		PromptPricePer1M:     input.PromptPricePer1M,
		CompletionPricePer1M: input.CompletionPricePer1M,
		CachePricePer1M:      input.CachePricePer1M,
	})
}

func (s *pricingService) DeletePricing(ctx context.Context, model string) error {
	return repository.DeleteModelPriceSetting(s.db.WithContext(ctx), model)
}
