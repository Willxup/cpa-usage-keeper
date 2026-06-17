package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestPricingServiceAllowsModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db)

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-sonnet" || setting.PricingStyle != "claude" || setting.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceStoresPricingForUsedModel(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-1",
		Model:       "claude-sonnet",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	service := NewPricingService(db)
	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-sonnet" || setting.PricingStyle != "claude" || setting.CompletionPricePer1M != 15 || setting.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected setting: %#v", setting)
	}

	usedModels, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list used models: %v", err)
	}
	if len(usedModels) != 1 || usedModels[0] != "claude-sonnet" {
		t.Fatalf("unexpected used models: %#v", usedModels)
	}
}

func TestPricingServiceRejectsUnknownPricingStyle(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-style",
		Model:       "claude-sonnet",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	service := NewPricingService(db)

	_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:        "claude-sonnet",
		PricingStyle: "legacy",
	})
	if err == nil || !strings.Contains(err.Error(), "pricing_style") {
		t.Fatalf("expected pricing style validation error, got %v", err)
	}
}

func TestPricingServiceListsModelsFromCPAWhenAvailable(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	logs := captureDebugLogs(t)

	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: " zeta-model "},
		{ID: "alpha-model"},
		{ID: "zeta-model"},
		{ID: ""},
	}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	expected := []string{"alpha-model", "zeta-model"}
	if strings.Join(modelsList, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected CPA models %#v, got %#v", expected, modelsList)
	}
	if !strings.Contains(logs.String(), "using CPA models endpoint") {
		t.Fatalf("expected CPA source debug log, got %q", logs.String())
	}
}

func TestPricingServiceFallsBackToLocalModelsWhenCPAFetchFails(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	logs := captureDebugLogs(t)

	service := NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	if len(modelsList) != 1 || modelsList[0] != "local-model" {
		t.Fatalf("expected local fallback model, got %#v", modelsList)
	}
	if !strings.Contains(logs.String(), "level=error") {
		t.Fatalf("expected fallback error log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "falling back to local usage aggregation") {
		t.Fatalf("expected fallback error log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "error=\"cpa unavailable\"") && !strings.Contains(logs.String(), "error=cpa unavailable") {
		t.Fatalf("expected fallback log to include original error, got %q", logs.String())
	}
}

func TestPricingServiceReturnsEmptyCPAListWithoutFallback(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: " "}}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(modelsList) != 0 {
		t.Fatalf("expected empty CPA model list, got %#v", modelsList)
	}
}

func TestPricingServiceAllowsPricingForCPAModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "claude-opus"}}}}})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "claude-opus",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "claude-opus" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceAllowsModelOutsideCPAModelList(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "cpa-model"}}}}})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                   "local-model",
		PricingStyle:            "claude",
		PromptPricePer1M:        3,
		CompletionPricePer1M:    15,
		CachePricePer1M:         0.3,
		CacheCreationPricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "local-model" || setting.PricingStyle != "claude" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestPricingServiceSavesPricingWhenCPAFetchFails(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "any-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.Model != "any-model" {
		t.Fatalf("unexpected setting: %#v", setting)
	}
}

func TestBuildPricingSyncPreviewMatchesMetadataModels(t *testing.T) {
	input := 2.5
	output := 10.0
	cacheRead := 1.25
	gptCacheRead := 0.25
	gptCacheWrite := 0.0
	claudeInput := 3.0
	claudeOutput := 15.0
	cacheWrite := 3.75
	zeroPrice := 0.0
	catalog := map[string]modelsDevProvider{
		"openai": {
			ID:   "openai",
			Name: "OpenAI",
			Models: map[string]modelsDevModel{
				"openai/gpt-4o": {
					ID:          "openai/gpt-4o",
					Name:        "GPT-4o",
					Family:      "gpt",
					LastUpdated: "2026-01-01",
					Cost: modelsDevCost{
						Input:     &input,
						Output:    &output,
						CacheRead: &cacheRead,
					},
				},
				"openai/gpt-5.4": {
					ID:          "openai/gpt-5.4",
					Name:        "GPT-5.4",
					Family:      "gpt",
					LastUpdated: "2026-01-01",
					Cost: modelsDevCost{
						Input:      &input,
						Output:     &output,
						CacheRead:  &gptCacheRead,
						CacheWrite: &gptCacheWrite,
					},
				},
			},
		},
		"anthropic": {
			ID:   "anthropic",
			Name: "Anthropic",
			Models: map[string]modelsDevModel{
				"anthropic/claude-sonnet-4": {
					ID:          "anthropic/claude-sonnet-4",
					Name:        "Claude Sonnet 4",
					Family:      "claude-sonnet",
					LastUpdated: "2026-01-01",
					Cost: modelsDevCost{
						Input:      &claudeInput,
						Output:     &claudeOutput,
						CacheRead:  &cacheRead,
						CacheWrite: &cacheWrite,
					},
				},
			},
		},
		"deepseek": {
			ID:   "deepseek",
			Name: "DeepSeek",
			Models: map[string]modelsDevModel{
				"deepseek-chat": {
					ID:          "deepseek-chat",
					Name:        "DeepSeek Chat",
					Family:      "deepseek",
					LastUpdated: "2026-01-01",
					Cost: modelsDevCost{
						Input:  &input,
						Output: &output,
					},
				},
			},
		},
		"302ai": {
			ID:   "302ai",
			Name: "302.AI",
			Models: map[string]modelsDevModel{
				"gpt-4o": {
					ID:          "gpt-4o",
					Name:        "GPT-4o",
					Family:      "gpt",
					LastUpdated: "2027-01-01",
					Cost: modelsDevCost{
						Input:  &claudeInput,
						Output: &claudeOutput,
					},
				},
				"deepseek-chat": {
					ID:          "deepseek-chat",
					Name:        "DeepSeek Chat",
					Family:      "deepseek",
					LastUpdated: "2027-01-01",
					Cost: modelsDevCost{
						Input:  &claudeInput,
						Output: &claudeOutput,
					},
				},
			},
		},
		"nebius": {
			ID:   "nebius",
			Name: "Nebius Token Factory",
			Models: map[string]modelsDevModel{
				"deepseek-ai/DeepSeek-V4-Pro": {
					ID:          "deepseek-ai/DeepSeek-V4-Pro",
					Name:        "DeepSeek V4 Pro",
					Family:      "deepseek",
					LastUpdated: "2026-04-24",
					Cost: modelsDevCost{
						Input:  &input,
						Output: &output,
					},
				},
				"deepseek-ai/DeepSeek-V4-Flash": {
					ID:          "deepseek-ai/DeepSeek-V4-Flash",
					Name:        "DeepSeek V4 Flash",
					Family:      "deepseek-flash",
					LastUpdated: "2026-04-24",
					Cost: modelsDevCost{
						Input:  &input,
						Output: &output,
					},
				},
			},
		},
		"zai": {
			ID:   "zai",
			Name: "Z.ai",
			Models: map[string]modelsDevModel{
				"zai-org/GLM-4.7-Flash": {
					ID:          "zai-org/GLM-4.7-Flash",
					Name:        "GLM-4.7-Flash",
					Family:      "glm-flash",
					LastUpdated: "2026-01-19",
					Cost: modelsDevCost{
						Input:  &input,
						Output: &output,
					},
				},
			},
		},
		"minimax-coding-plan": {
			ID:   "minimax-coding-plan",
			Name: "MiniMax Coding Plan",
			Models: map[string]modelsDevModel{
				"MiniMax-M3": {
					ID:          "MiniMax-M3",
					Name:        "MiniMax-M3",
					Family:      "minimax",
					LastUpdated: "2026-03-01",
					Cost: modelsDevCost{
						Input:  &zeroPrice,
						Output: &zeroPrice,
					},
				},
			},
		},
		"vercel": {
			ID:   "vercel",
			Name: "Vercel",
			Models: map[string]modelsDevModel{
				"minimax/minimax-m3": {
					ID:          "minimax/minimax-m3",
					Name:        "MiniMax M3",
					Family:      "minimax",
					LastUpdated: "2026-03-01",
					Cost: modelsDevCost{
						Input:  &input,
						Output: &output,
					},
				},
			},
		},
	}

	preview, err := buildPricingSyncPreviewFromCatalog([]string{
		"openai/gpt-4o",
		"Claude Sonnet 4",
		"deepseek-chat",
		"gpt-5.4",
		"deepseek-v4-pro",
		"DeepSeek V4 Flash",
		"GLM-4.7-Flash",
		"minimax-m3",
		"missing-model",
	}, catalog, "https://models.dev/api.json")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}

	if preview.Source != "Models.dev" || preview.SourceURL != "https://models.dev/api.json" {
		t.Fatalf("unexpected preview source: %#v", preview)
	}
	if preview.MetadataModels != 11 {
		t.Fatalf("expected metadata model count, got %d", preview.MetadataModels)
	}
	if len(preview.Matches) != 8 {
		t.Fatalf("expected 8 matches, got %#v", preview.Matches)
	}
	matchesByModel := make(map[string]servicedto.PricingSyncMatch, len(preview.Matches))
	for _, match := range preview.Matches {
		matchesByModel[match.Model] = match
	}
	if match := matchesByModel["Claude Sonnet 4"]; match.PricingStyle != "claude" || match.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("unexpected claude match: %#v", match)
	}
	if match := matchesByModel["openai/gpt-4o"]; match.MatchedModel != "openai/gpt-4o" || match.MatchType != "index_exact" || match.SourceProviderID != "openai" {
		t.Fatalf("unexpected gpt match: %#v", match)
	}
	if match := matchesByModel["deepseek-chat"]; match.SourceProviderID != "deepseek" {
		t.Fatalf("unexpected deepseek official priority match: %#v", match)
	}
	if match := matchesByModel["gpt-5.4"]; match.PricingStyle != "openai" || match.CachePricePer1M != 0.25 || match.CacheCreationPricePer1M != 0 {
		t.Fatalf("unexpected openai cache match: %#v", match)
	}
	if match := matchesByModel["deepseek-v4-pro"]; match.MatchedModel != "deepseek-ai/DeepSeek-V4-Pro" || match.SourceProviderID != "nebius" {
		t.Fatalf("unexpected deepseek index match: %#v", match)
	}
	if match := matchesByModel["DeepSeek V4 Flash"]; match.MatchedModel != "deepseek-ai/DeepSeek-V4-Flash" || match.SourceProviderID != "nebius" {
		t.Fatalf("unexpected deepseek flash match: %#v", match)
	}
	if match := matchesByModel["GLM-4.7-Flash"]; match.MatchedModel != "zai-org/GLM-4.7-Flash" || match.SourceProviderID != "zai" {
		t.Fatalf("unexpected glm match: %#v", match)
	}
	if match := matchesByModel["minimax-m3"]; match.MatchedModel != "minimax/minimax-m3" || match.SourceProviderID != "vercel" || match.PromptPricePer1M == 0 {
		t.Fatalf("unexpected minimax plan fallback match: %#v", match)
	}
	if len(preview.UnmatchedModels) != 1 || preview.UnmatchedModels[0] != "missing-model" {
		t.Fatalf("unexpected unmatched models: %#v", preview.UnmatchedModels)
	}
}

func TestParseLiteLLMPricingCatalogConvertsPerTokenPricesToPerMillion(t *testing.T) {
	catalog, err := parseLiteLLMPricingCatalog([]byte(`{
		"sample_spec": {"input_cost_per_token": 0, "output_cost_per_token": 0},
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"output_cost_per_token": 0.000015,
			"cache_read_input_token_cost": 0.00000025
		},
		"image-only": {"output_cost_per_image": 0.04}
	}`))
	if err != nil {
		t.Fatalf("parse LiteLLM catalog: %v", err)
	}
	price, ok := catalog["gpt-5.4"]
	if !ok {
		t.Fatalf("expected gpt-5.4 price in catalog: %#v", catalog)
	}
	if price.PromptPricePer1M != 2.5 || price.CompletionPricePer1M != 15 || price.CachePricePer1M != 0.25 {
		t.Fatalf("unexpected converted price: %#v", price)
	}
	if _, ok := catalog["sample_spec"]; ok {
		t.Fatal("expected sample_spec to be skipped")
	}
	if _, ok := catalog["image-only"]; ok {
		t.Fatal("expected non-token pricing to be skipped")
	}
}

func TestPricingServiceSyncsLiteLLMPricesForUsedModelsWithoutOverwritingManual(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "evt-gpt-54", Model: "gpt-5.4", Timestamp: time.Unix(1, 0), APIGroupKey: "provider-a"},
		{EventKey: "evt-manual", Model: "manual-model", Timestamp: time.Unix(2, 0), APIGroupKey: "provider-a"},
		{EventKey: "evt-models-dev", Model: "models-dev-model", Timestamp: time.Unix(3, 0), APIGroupKey: "provider-a"},
		{EventKey: "evt-missing", Model: "missing-model", Timestamp: time.Unix(3, 0), APIGroupKey: "provider-a"},
	}); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "manual-model",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 2,
		CachePricePer1M:      0.5,
		Source:               repository.ModelPriceSourceManual,
	}); err != nil {
		t.Fatalf("seed manual pricing: %v", err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "models-dev-model",
		PromptPricePer1M:     1.5,
		CompletionPricePer1M: 2.5,
		CachePricePer1M:      0.75,
		Source:               "Models.dev",
	}); err != nil {
		t.Fatalf("seed manually applied sync pricing: %v", err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "old-litellm-model",
		PromptPricePer1M:     0.1,
		CompletionPricePer1M: 0.2,
		CachePricePer1M:      0.03,
		Source:               modelPriceSourceLiteLLM,
	}); err != nil {
		t.Fatalf("seed LiteLLM pricing: %v", err)
	}

	service := &pricingService{
		db: db,
		catalogFetcher: stubPricingCatalogFetcher{
			sourceURL: "https://example.test/litellm.json",
			entries: map[string]liteLLMPricingCatalogPrice{
				"gpt-5.4":           {PromptPricePer1M: 2.5, CompletionPricePer1M: 15, CachePricePer1M: 0.25},
				"manual-model":      {PromptPricePer1M: 9, CompletionPricePer1M: 9, CachePricePer1M: 9},
				"models-dev-model":  {PromptPricePer1M: 8, CompletionPricePer1M: 8, CachePricePer1M: 8},
				"old-litellm-model": {PromptPricePer1M: 3, CompletionPricePer1M: 4, CachePricePer1M: 0.4},
			},
		},
	}
	result, err := service.SyncPricing(context.Background(), servicedto.SyncPricingInput{OverwriteManual: true})
	if err != nil {
		t.Fatalf("sync pricing: %v", err)
	}
	if strings.Join(result.CreatedModels, ",") != "gpt-5.4" {
		t.Fatalf("expected gpt-5.4 to be created, got %#v", result.CreatedModels)
	}
	if strings.Join(result.UpdatedModels, ",") != "old-litellm-model" {
		t.Fatalf("expected old-litellm-model to be updated, got %#v", result.UpdatedModels)
	}
	if strings.Join(result.SkippedManualModels, ",") != "manual-model,models-dev-model" {
		t.Fatalf("expected manual and manually applied sync models to be skipped, got %#v", result.SkippedManualModels)
	}
	if strings.Join(result.MissingModels, ",") != "missing-model" {
		t.Fatalf("expected missing-model to be reported missing, got %#v", result.MissingModels)
	}

	settings, err := repository.ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("list pricing: %v", err)
	}
	byModel := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		byModel[setting.Model] = setting
	}
	if byModel["gpt-5.4"].Source != modelPriceSourceLiteLLM || byModel["gpt-5.4"].SourceURL != "https://example.test/litellm.json" || byModel["gpt-5.4"].SyncedAt == nil {
		t.Fatalf("expected synced LiteLLM metadata, got %#v", byModel["gpt-5.4"])
	}
	if byModel["manual-model"].PromptPricePer1M != 1 || byModel["manual-model"].Source != repository.ModelPriceSourceManual {
		t.Fatalf("expected manual pricing to be preserved, got %#v", byModel["manual-model"])
	}
	if byModel["models-dev-model"].PromptPricePer1M != 1.5 || byModel["models-dev-model"].Source != "Models.dev" {
		t.Fatalf("expected manually applied sync pricing to be preserved, got %#v", byModel["models-dev-model"])
	}
	if byModel["old-litellm-model"].PromptPricePer1M != 3 || byModel["old-litellm-model"].Source != modelPriceSourceLiteLLM {
		t.Fatalf("expected LiteLLM pricing to be updated, got %#v", byModel["old-litellm-model"])
	}
}

type stubModelsFetcher struct {
	result *response.ModelsResult
	err    error
}

func (s stubModelsFetcher) FetchModels(context.Context) (*response.ModelsResult, error) {
	return s.result, s.err
}

type stubPricingCatalogFetcher struct {
	sourceURL string
	entries   map[string]liteLLMPricingCatalogPrice
	err       error
}

func (s stubPricingCatalogFetcher) FetchPricingCatalog(context.Context) (map[string]liteLLMPricingCatalogPrice, error) {
	return s.entries, s.err
}

func (s stubPricingCatalogFetcher) SourceURL() string {
	return s.sourceURL
}

func (s stubPricingCatalogFetcher) SourceName() string {
	return modelPriceSourceLiteLLM
}

func captureDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousOutput := logrus.StandardLogger().Out
	previousLevel := logrus.GetLevel()
	var logs bytes.Buffer
	logrus.SetOutput(&logs)
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func openPricingServiceTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "pricing-service.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
