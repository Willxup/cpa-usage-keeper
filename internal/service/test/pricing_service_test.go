package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestPricingServiceAllowsModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := service.NewPricingService(db)

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

func TestPricingServiceDefaultsPriceMultiplierToOne(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := service.NewPricingService(db)

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "default-multiplier-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if setting.PriceMultiplier == nil || *setting.PriceMultiplier != 1 {
		t.Fatalf("expected omitted price multiplier to default to 1, got %+v", setting.PriceMultiplier)
	}
}

func TestPricingServiceAllowsZeroPriceMultiplier(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := service.NewPricingService(db)
	zero := 0.0

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "zero-multiplier-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
		PriceMultiplier:      &zero,
	})
	if err != nil {
		t.Fatalf("update pricing with zero multiplier: %v", err)
	}
	if setting.PriceMultiplier == nil || *setting.PriceMultiplier != 0 {
		t.Fatalf("expected zero price multiplier, got %+v", setting.PriceMultiplier)
	}
}

func TestPricingServiceRejectsInvalidPriceMultiplier(t *testing.T) {
	for name, value := range map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"infinite": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			db := openPricingServiceTestDatabase(t)
			service := service.NewPricingService(db)

			_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
				Model:                name + "-multiplier-model",
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CachePricePer1M:      0.3,
				PriceMultiplier:      &value,
			})
			if err == nil || !strings.Contains(err.Error(), "price_multiplier") {
				t.Fatalf("expected price_multiplier validation error, got %v", err)
			}
		})
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

	service := service.NewPricingService(db)
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
	service := service.NewPricingService(db)

	_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:        "claude-sonnet",
		PricingStyle: "legacy",
	})
	if err == nil || !strings.Contains(err.Error(), "pricing_style") {
		t.Fatalf("expected pricing style validation error, got %v", err)
	}
}

func TestPricingServiceMergesCPAAndLocalModelsWhenCPAAvailable(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{
			EventKey:    "evt-local",
			Model:       "local-model",
			Timestamp:   time.Unix(1, 0),
			APIGroupKey: "provider-a",
		},
		{
			EventKey:    "evt-overlap",
			Model:       "alpha-model",
			Timestamp:   time.Unix(2, 0),
			APIGroupKey: "provider-a",
		},
	}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}
	logs := captureDebugLogs(t)

	service := service.NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: " zeta-model "},
		{ID: "alpha-model"},
		{ID: "zeta-model"},
		{ID: ""},
	}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	expected := []string{"alpha-model", "local-model", "zeta-model"}
	if strings.Join(modelsList, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected merged CPA and local models %#v, got %#v", expected, modelsList)
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

	service := service.NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})
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

func TestPricingServiceKeepsLocalModelsWhenCPAListIsEmpty(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "evt-local",
		Model:       "local-model",
		Timestamp:   time.Unix(1, 0),
		APIGroupKey: "provider-a",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	service := service.NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{}}}})
	modelsList, err := service.ListUsedModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(modelsList) != 1 || modelsList[0] != "local-model" {
		t.Fatalf("expected local model when CPA list is empty, got %#v", modelsList)
	}
}

func TestPricingServiceAllowsPricingForCPAModelWithoutUsage(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	service := service.NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "claude-opus"}}}}})

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
	service := service.NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "cpa-model"}}}}})

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
	service := service.NewPricingService(db, stubModelsFetcher{err: errors.New("cpa unavailable")})

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
	transport := http.DefaultTransport
	http.DefaultTransport = pricingCatalogTransport{body: testPricingCatalogJSON}
	t.Cleanup(func() {
		http.DefaultTransport = transport
	})

	db := openPricingServiceTestDatabase(t)
	service := service.NewPricingService(db, stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: "openai/gpt-4o"},
		{ID: "Claude Sonnet 4"},
		{ID: "deepseek-chat"},
		{ID: "gpt-5.4"},
		{ID: "deepseek-v4-pro"},
		{ID: "DeepSeek V4 Flash"},
		{ID: "GLM-4.7-Flash"},
		{ID: "minimax-m3"},
		{ID: "missing-model"},
	}}}})

	preview, err := service.PreviewPricingSync(context.Background())
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

const testPricingCatalogJSON = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "openai/gpt-4o": {
        "id": "openai/gpt-4o",
        "name": "GPT-4o",
        "family": "gpt",
        "last_updated": "2026-01-01",
        "cost": {"input": 2.5, "output": 10, "cache_read": 1.25}
      },
      "openai/gpt-5.4": {
        "id": "openai/gpt-5.4",
        "name": "GPT-5.4",
        "family": "gpt",
        "last_updated": "2026-01-01",
        "cost": {"input": 2.5, "output": 10, "cache_read": 0.25, "cache_write": 0}
      }
    }
  },
  "anthropic": {
    "id": "anthropic",
    "name": "Anthropic",
    "models": {
      "anthropic/claude-sonnet-4": {
        "id": "anthropic/claude-sonnet-4",
        "name": "Claude Sonnet 4",
        "family": "claude-sonnet",
        "last_updated": "2026-01-01",
        "cost": {"input": 3, "output": 15, "cache_read": 1.25, "cache_write": 3.75}
      }
    }
  },
  "deepseek": {
    "id": "deepseek",
    "name": "DeepSeek",
    "models": {
      "deepseek-chat": {
        "id": "deepseek-chat",
        "name": "DeepSeek Chat",
        "family": "deepseek",
        "last_updated": "2026-01-01",
        "cost": {"input": 2.5, "output": 10}
      }
    }
  },
  "302ai": {
    "id": "302ai",
    "name": "302.AI",
    "models": {
      "gpt-4o": {
        "id": "gpt-4o",
        "name": "GPT-4o",
        "family": "gpt",
        "last_updated": "2027-01-01",
        "cost": {"input": 3, "output": 15}
      },
      "deepseek-chat": {
        "id": "deepseek-chat",
        "name": "DeepSeek Chat",
        "family": "deepseek",
        "last_updated": "2027-01-01",
        "cost": {"input": 3, "output": 15}
      }
    }
  },
  "nebius": {
    "id": "nebius",
    "name": "Nebius Token Factory",
    "models": {
      "deepseek-ai/DeepSeek-V4-Pro": {
        "id": "deepseek-ai/DeepSeek-V4-Pro",
        "name": "DeepSeek V4 Pro",
        "family": "deepseek",
        "last_updated": "2026-04-24",
        "cost": {"input": 2.5, "output": 10}
      },
      "deepseek-ai/DeepSeek-V4-Flash": {
        "id": "deepseek-ai/DeepSeek-V4-Flash",
        "name": "DeepSeek V4 Flash",
        "family": "deepseek-flash",
        "last_updated": "2026-04-24",
        "cost": {"input": 2.5, "output": 10}
      }
    }
  },
  "zai": {
    "id": "zai",
    "name": "Z.ai",
    "models": {
      "zai-org/GLM-4.7-Flash": {
        "id": "zai-org/GLM-4.7-Flash",
        "name": "GLM-4.7-Flash",
        "family": "glm-flash",
        "last_updated": "2026-01-19",
        "cost": {"input": 2.5, "output": 10}
      }
    }
  },
  "minimax-coding-plan": {
    "id": "minimax-coding-plan",
    "name": "MiniMax Coding Plan",
    "models": {
      "MiniMax-M3": {
        "id": "MiniMax-M3",
        "name": "MiniMax-M3",
        "family": "minimax",
        "last_updated": "2026-03-01",
        "cost": {"input": 0, "output": 0}
      }
    }
  },
  "vercel": {
    "id": "vercel",
    "name": "Vercel",
    "models": {
      "minimax/minimax-m3": {
        "id": "minimax/minimax-m3",
        "name": "MiniMax M3",
        "family": "minimax",
        "last_updated": "2026-03-01",
        "cost": {"input": 2.5, "output": 10}
      }
    }
  }
}`

type pricingCatalogTransport struct {
	body string
}

func (t pricingCatalogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != "https://models.dev/api.json" {
		return nil, errors.New("unexpected pricing catalog request: " + request.URL.String())
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    request,
	}, nil
}

type stubModelsFetcher struct {
	result *response.ModelsResult
	err    error
}

func (s stubModelsFetcher) FetchModels(context.Context) (*response.ModelsResult, error) {
	return s.result, s.err
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
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}
