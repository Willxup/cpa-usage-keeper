package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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
	SyncPricing(context.Context, servicedto.SyncPricingInput) (servicedto.PricingSyncResult, error)
	GetPricingSyncStatus(context.Context) servicedto.PricingSyncStatus
}

type ModelsFetcher interface {
	FetchModels(context.Context) (*response.ModelsResult, error)
}

type pricingService struct {
	db             *gorm.DB
	modelsFetcher  ModelsFetcher
	catalogFetcher PricingCatalogFetcher

	syncMu      sync.Mutex
	syncStatus  servicedto.PricingSyncStatus
	syncStatusM sync.Mutex
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

	return repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
		Model:                   modelName,
		PricingStyle:            pricingStyle,
		PromptPricePer1M:        input.PromptPricePer1M,
		CompletionPricePer1M:    input.CompletionPricePer1M,
		CachePricePer1M:         input.CachePricePer1M,
		CacheCreationPricePer1M: input.CacheCreationPricePer1M,
		Source:                  repository.ModelPriceSourceManual,
	})
}

func (s *pricingService) DeletePricing(_ context.Context, model string) error {
	return repository.DeleteModelPriceSetting(s.db, model)
}

func (s *pricingService) SyncPricing(ctx context.Context, input servicedto.SyncPricingInput) (servicedto.PricingSyncResult, error) {
	if s == nil || s.db == nil {
		return servicedto.PricingSyncResult{}, fmt.Errorf("pricing service is not configured")
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.setPricingSyncRunning(true)
	defer s.setPricingSyncRunning(false)

	result, err := s.syncPricing(ctx, input)
	s.recordPricingSyncResult(result, err)
	return result, err
}

func (s *pricingService) GetPricingSyncStatus(context.Context) servicedto.PricingSyncStatus {
	s.syncStatusM.Lock()
	defer s.syncStatusM.Unlock()
	status := s.syncStatus
	if status.LastResult != nil {
		copied := *status.LastResult
		status.LastResult = &copied
	}
	return status
}

func (s *pricingService) syncPricing(ctx context.Context, _ servicedto.SyncPricingInput) (servicedto.PricingSyncResult, error) {
	fetcher := s.catalogFetcher
	if fetcher == nil {
		fetcher = newDefaultLiteLLMPricingCatalogFetcher()
	}
	now := time.Now()
	result := servicedto.PricingSyncResult{
		Source:              fetcher.SourceName(),
		SourceURL:           fetcher.SourceURL(),
		SyncedAt:            now,
		CreatedModels:       []string{},
		UpdatedModels:       []string{},
		MissingModels:       []string{},
		SkippedManualModels: []string{},
	}

	models, err := repository.ListUsedModels(s.db)
	if err != nil {
		return result, err
	}
	settings, err := repository.ListModelPriceSettings(s.db)
	if err != nil {
		return result, err
	}
	existingByModel := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		model := strings.TrimSpace(setting.Model)
		if model == "" {
			continue
		}
		existingByModel[model] = setting
		if strings.TrimSpace(setting.Source) == modelPriceSourceLiteLLM {
			models = append(models, model)
		}
	}
	models = normalizePricingSyncModels(models)
	result.ModelsChecked = len(models)

	catalog, err := fetcher.FetchPricingCatalog(ctx)
	if err != nil {
		return result, err
	}

	for _, model := range models {
		pricing, ok := catalog[model]
		if !ok {
			result.MissingModels = append(result.MissingModels, model)
			continue
		}
		existing, exists := existingByModel[model]
		source := strings.TrimSpace(existing.Source)
		if exists && source != modelPriceSourceLiteLLM {
			result.SkippedManualModels = append(result.SkippedManualModels, model)
			continue
		}
		syncedAt := now
		if _, err := repository.UpsertModelPriceSetting(s.db, repodto.ModelPriceSettingInput{
			Model:                   model,
			PricingStyle:            entities.ModelPricingStyleOpenAI,
			PromptPricePer1M:        pricing.PromptPricePer1M,
			CompletionPricePer1M:    pricing.CompletionPricePer1M,
			CachePricePer1M:         pricing.CachePricePer1M,
			CacheCreationPricePer1M: 0,
			Source:                  fetcher.SourceName(),
			SourceURL:               fetcher.SourceURL(),
			SyncedAt:                &syncedAt,
		}); err != nil {
			return result, err
		}
		if exists {
			result.UpdatedModels = append(result.UpdatedModels, model)
		} else {
			result.CreatedModels = append(result.CreatedModels, model)
		}
	}
	sort.Strings(result.CreatedModels)
	sort.Strings(result.UpdatedModels)
	sort.Strings(result.MissingModels)
	sort.Strings(result.SkippedManualModels)
	return result, nil
}

func normalizePricingSyncModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func (s *pricingService) setPricingSyncRunning(running bool) {
	s.syncStatusM.Lock()
	defer s.syncStatusM.Unlock()
	s.syncStatus.Running = running
}

func (s *pricingService) recordPricingSyncResult(result servicedto.PricingSyncResult, err error) {
	s.syncStatusM.Lock()
	defer s.syncStatusM.Unlock()
	if err != nil {
		s.syncStatus.LastError = err.Error()
		return
	}
	s.syncStatus.LastError = ""
	syncedAt := result.SyncedAt
	s.syncStatus.LastSyncedAt = &syncedAt
	copied := result
	s.syncStatus.LastResult = &copied
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
