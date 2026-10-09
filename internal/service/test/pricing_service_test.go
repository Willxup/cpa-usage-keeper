package test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestPricingServiceStoresPricingRegardlessOfModelSource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		used    bool
		fetcher service.ModelsFetcher
	}{
		{name: "without usage"},
		{name: "used model", used: true},
		{name: "CPA model", fetcher: stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "claude-sonnet"}}}}}},
		{name: "outside CPA list", fetcher: stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "other-model"}}}}}},
		{name: "CPA failure", fetcher: stubModelsFetcher{err: errors.New("cpa unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			if tc.used {
				seedPricingUsageModels(t, db, "claude-sonnet")
			}
			provider := service.NewPricingService(db, emptyPricingCatalogForTest(), tc.fetcher)
			setting, err := provider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
				Model: "claude-sonnet", PricingStyle: "claude", PromptPricePer1M: 3,
				CompletionPricePer1M: 15, CacheReadPricePer1M: 0.3, CacheWritePricePer1M: 3.75,
			})
			if err != nil {
				t.Fatalf("update pricing: %v", err)
			}
			if setting.Model != "claude-sonnet" || setting.PricingStyle != "claude" || setting.CompletionPricePer1M != 15 || setting.CacheWritePricePer1M != 3.75 {
				t.Fatalf("unexpected setting: %#v", setting)
			}
			if tc.used {
				usedModels, err := provider.ListUsedModels(context.Background())
				if err != nil || !slices.Equal(usedModels, []string{"claude-sonnet"}) {
					t.Fatalf("used models = %v, err = %v", usedModels, err)
				}
			}
		})
	}
}

func TestPricingServicePreservesOpenAICacheReadAndWritePrices(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	service := service.NewPricingService(db, emptyPricingCatalogForTest())

	setting, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:                "gpt-5.6-terra",
		PricingStyle:         "openai",
		PromptPricePer1M:     2.5,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.25,
		CacheWritePricePer1M: 3.125,
	})
	if err != nil {
		t.Fatalf("update OpenAI pricing: %v", err)
	}
	if setting.PricingStyle != "openai" || setting.CacheReadPricePer1M != 0.25 || setting.CacheWritePricePer1M != 3.125 {
		t.Fatalf("unexpected OpenAI cache pricing: %#v", setting)
	}
}

func TestPricingServiceDefaultsAndPreservesPriceMultiplier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input *float64
		want  float64
	}{
		{name: "omitted", want: 1},
		{name: "zero", input: new(0.0), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := service.NewPricingService(openUsageServiceTestDatabase(t), emptyPricingCatalogForTest())
			setting, err := provider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
				Model: "multiplier-model", PromptPricePer1M: 3, CompletionPricePer1M: 15,
				CacheReadPricePer1M: 0.3, PriceMultiplier: tc.input,
			})
			if err != nil {
				t.Fatalf("update pricing: %v", err)
			}
			if setting.PriceMultiplier == nil || *setting.PriceMultiplier != tc.want {
				t.Fatalf("price multiplier = %v, want %v", setting.PriceMultiplier, tc.want)
			}
		})
	}
}

func TestPricingServiceRejectsInvalidPriceMultiplier(t *testing.T) {
	for name, value := range map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"infinite": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			service := service.NewPricingService(db, emptyPricingCatalogForTest())

			_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
				Model:                name + "-multiplier-model",
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				PriceMultiplier:      &value,
			})
			if err == nil || !strings.Contains(err.Error(), "price_multiplier") {
				t.Fatalf("expected price_multiplier validation error, got %v", err)
			}
		})
	}
}

func TestPricingServiceRejectsUnknownPricingStyle(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	service := service.NewPricingService(db, emptyPricingCatalogForTest())

	_, err := service.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:        "claude-sonnet",
		PricingStyle: "legacy",
	})
	if err == nil || !strings.Contains(err.Error(), "pricing_style") {
		t.Fatalf("expected pricing style validation error, got %v", err)
	}
}

func TestPricingServiceMergesOrFallsBackToLocalModels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		local   []string
		fetcher stubModelsFetcher
		want    []string
		logs    []string
	}{
		{
			name: "merge and normalize", local: []string{"local-model", "alpha-model"},
			fetcher: stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
				{ID: " zeta-model "}, {ID: "alpha-model"}, {ID: "zeta-model"}, {ID: ""},
			}}}},
			want: []string{"alpha-model", "local-model", "zeta-model"}, logs: []string{"using CPA models endpoint"},
		},
		{
			name: "fetch failure", local: []string{"local-model"},
			fetcher: stubModelsFetcher{err: errors.New("cpa unavailable")}, want: []string{"local-model"},
			logs: []string{"level=error", "falling back to local usage aggregation", `error="cpa unavailable"`},
		},
		{
			name: "empty CPA list", local: []string{"local-model"},
			fetcher: stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{}}}},
			want:    []string{"local-model"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			seedPricingUsageModels(t, db, tc.local...)
			logs := captureSyncCleanupLogs(t, logrus.DebugLevel)
			provider := service.NewPricingService(db, emptyPricingCatalogForTest(), tc.fetcher)
			got, err := provider.ListUsedModels(context.Background())
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("models = %v, err = %v; want %v", got, err, tc.want)
			}
			for _, message := range tc.logs {
				if !strings.Contains(logs.String(), message) {
					t.Fatalf("missing %q in logs: %s", message, logs.String())
				}
			}
		})
	}
}

func TestBuildPricingSyncPreviewMatchesMetadataModels(t *testing.T) {
	usePricingCatalogTransport(t, testPricingCatalogJSON)

	db := openUsageServiceTestDatabase(t)
	service := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: "openai/gpt-4o"},
		{ID: "Claude Sonnet 4"},
		{ID: "deepseek-chat"},
		{ID: "gpt-5.4"},
		{ID: "gpt-5.6-terra"},
		{ID: "deepseek-v4-pro"},
		{ID: "DeepSeek V4 Flash"},
		{ID: "GLM-4.7-Flash"},
		{ID: "minimax-m3"},
		{ID: "missing-model"},
	}}}})

	preview, err := service.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}

	if preview.Source != "Models.dev" || preview.SourceURL != "https://models.dev/api.json" {
		t.Fatalf("unexpected preview source: %#v", preview)
	}
	if preview.MetadataModels != 13 {
		t.Fatalf("expected metadata model count, got %d", preview.MetadataModels)
	}
	if len(preview.Matches) != 9 {
		t.Fatalf("expected 9 matches, got %#v", preview.Matches)
	}
	matchesByModel := make(map[string]servicedto.PricingSyncMatch, len(preview.Matches))
	for _, match := range preview.Matches {
		matchesByModel[match.Model] = match
	}
	if match := matchesByModel["Claude Sonnet 4"]; match.PricingStyle != "claude" || match.CacheWritePricePer1M != 3.75 {
		t.Fatalf("unexpected claude match: %#v", match)
	}
	if match := matchesByModel["openai/gpt-4o"]; match.MatchedModel != "openai/gpt-4o" || match.MatchType != "index_suffix" || match.SourceProviderID != "openai" {
		t.Fatalf("unexpected gpt match: %#v", match)
	}
	if match := matchesByModel["deepseek-chat"]; match.SourceProviderID != "deepseek" {
		t.Fatalf("unexpected deepseek official priority match: %#v", match)
	}
	if match := matchesByModel["gpt-5.4"]; match.PricingStyle != "openai" || match.CacheReadPricePer1M != 0.25 || match.CacheWritePricePer1M != 0 {
		t.Fatalf("unexpected openai cache match: %#v", match)
	}
	if match := matchesByModel["gpt-5.6-terra"]; match.MatchedModel != "gpt-5.6-terra" || match.MatchType != "index_exact" || match.SourceProviderID != "openai" || match.PricingStyle != "openai" || match.CacheReadPricePer1M != 0.25 || match.CacheWritePricePer1M != 3.125 {
		t.Fatalf("unexpected official OpenAI cache-write match: %#v", match)
	}
	if match := matchesByModel["openai/gpt-4o"]; match.CacheWritePricePer1M != 0 {
		t.Fatalf("expected missing OpenAI cache_write metadata to default to zero, got %#v", match)
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

func TestBuildPricingSyncPreviewStripsCPAPrefixBeforeMatchingModelsDev(t *testing.T) {
	usePricingCatalogTransport(t, `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"models": {
				"gpt-5.6-terra": {
					"id": "gpt-5.6-terra",
					"name": "GPT-5.6 Terra",
					"family": "gpt",
					"cost": {"input": 2.5, "output": 15, "cache_read": 0.25, "cache_write": 3.125}
				}
			}
		},
		"vercel": {
			"id": "vercel",
			"name": "Vercel",
			"models": {
				"openai/gpt-5.6-terra": {
					"id": "openai/gpt-5.6-terra",
					"name": "OpenAI GPT-5.6 Terra",
					"family": "gpt",
					"cost": {"input": 9, "output": 99, "cache_read": 0.9, "cache_write": 9.9}
				}
			}
		}
	}`)

	db := openUsageServiceTestDatabase(t)
	pricingService := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "openai/gpt-5.6-terra"}}}}})
	preview, err := pricingService.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}
	if len(preview.Matches) != 1 {
		t.Fatalf("expected one provider-hinted match, got %#v", preview)
	}
	match := preview.Matches[0]
	if match.SourceProviderID != "openai" || match.MatchedModel != "gpt-5.6-terra" || match.CacheReadPricePer1M != 0.25 || match.CacheWritePricePer1M != 3.125 {
		t.Fatalf("expected stripped model ID to select OpenAI pricing, got %#v", match)
	}
}

func TestBuildPricingSyncPreviewIgnoresCustomCPAPrefixForProviderSelection(t *testing.T) {
	usePricingCatalogTransport(t, `{
		"mimo": {
			"id": "mimo",
			"name": "Custom MIMO Gateway",
			"models": {
				"mimo-v2.5-pro": {
					"id": "mimo-v2.5-pro",
					"name": "MiMo V2.5 Pro",
					"family": "mimo",
					"cost": {"input": 9, "output": 18}
				}
			}
		},
		"xiaomi": {
			"id": "xiaomi",
			"name": "Xiaomi",
			"models": {
				"mimo-v2.5-pro": {
					"id": "mimo-v2.5-pro",
					"name": "MiMo V2.5 Pro",
					"family": "mimo",
					"cost": {"input": 0.435, "output": 0.87}
				}
			}
		}
	}`)

	db := openUsageServiceTestDatabase(t)
	pricingService := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "MIMO/mimo-v2.5-pro"}}}}})
	preview, err := pricingService.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}
	if len(preview.Matches) != 1 {
		t.Fatalf("expected one custom-prefix match, got %#v", preview)
	}
	match := preview.Matches[0]
	if match.SourceProviderID != "xiaomi" || match.MatchedModel != "mimo-v2.5-pro" || match.PromptPricePer1M != 0.435 || match.CompletionPricePer1M != 0.87 {
		t.Fatalf("expected custom CPA prefix not to affect provider ranking, got %#v", match)
	}
}

func TestBuildPricingSyncPreviewKeepsCandidatesWhenPrefixProviderLacksModel(t *testing.T) {
	usePricingCatalogTransport(t, `{
		"deepseek": {
			"id": "deepseek",
			"name": "DeepSeek",
			"models": {
				"deepseek-chat": {
					"id": "deepseek-chat",
					"name": "DeepSeek Chat",
					"family": "deepseek",
					"cost": {"input": 0.14, "output": 0.28}
				}
			}
		},
		"openrouter": {
			"id": "openrouter",
			"name": "OpenRouter",
			"models": {
				"deepseek/deepseek-v3.2": {
					"id": "deepseek/deepseek-v3.2",
					"name": "DeepSeek V3.2",
					"family": "deepseek",
					"cost": {"input": 0.2145, "output": 0.32175}
				}
			}
		}
	}`)

	db := openUsageServiceTestDatabase(t)
	pricingService := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "deepseek/deepseek-v3.2"}}}}})
	preview, err := pricingService.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}
	if len(preview.Matches) != 1 {
		t.Fatalf("expected namespace-prefixed model to keep fallback candidates, got %#v", preview)
	}
	match := preview.Matches[0]
	if match.SourceProviderID != "openrouter" || match.MatchedModel != "deepseek/deepseek-v3.2" {
		t.Fatalf("expected OpenRouter fallback candidate, got %#v", match)
	}
}

func TestBuildPricingSyncPreviewDefaultsMissingCachePricesToZero(t *testing.T) {
	usePricingCatalogTransport(t, `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"models": {
				"gpt-no-cache-price": {
					"id": "gpt-no-cache-price",
					"name": "GPT No Cache Price",
					"family": "gpt",
					"cost": {"input": 2.5, "output": 10}
				}
			}
		}
	}`)

	db := openUsageServiceTestDatabase(t)
	pricingService := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "gpt-no-cache-price"}}}}})
	preview, err := pricingService.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}
	if len(preview.Matches) != 1 {
		t.Fatalf("expected one match, got %#v", preview)
	}
	match := preview.Matches[0]
	if match.CacheReadPricePer1M != 0 || match.CacheWritePricePer1M != 0 {
		t.Fatalf("expected missing cache prices to default to zero, got %#v", match)
	}
}

func TestBuildPricingSyncPreviewRejectsNegativeOpenAICacheWrite(t *testing.T) {
	usePricingCatalogTransport(t, `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"models": {
				"gpt-negative-write": {
					"id": "gpt-negative-write",
					"name": "GPT Negative Write",
					"family": "gpt",
					"cost": {"input": 2.5, "output": 10, "cache_read": 0.25, "cache_write": -1}
				}
			}
		}
	}`)

	db := openUsageServiceTestDatabase(t)
	service := service.NewPricingService(db, emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "gpt-negative-write"}}}}})
	preview, err := service.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatalf("build pricing sync preview: %v", err)
	}
	if len(preview.Matches) != 0 || len(preview.UnmatchedModels) != 1 || preview.UnmatchedModels[0] != "gpt-negative-write" {
		t.Fatalf("expected negative cache_write candidate to be rejected, got %#v", preview)
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
		},
		"gpt-5.6-terra": {
			"id": "gpt-5.6-terra",
			"name": "GPT-5.6 Terra",
			"family": "gpt-mini",
			"last_updated": "2026-07-09",
			"cost": {
				"input": 2.5,
				"output": 15,
				"cache_read": 0.25,
				"cache_write": 3.125,
				"tiers": [{"input": 5, "output": 22.5, "cache_read": 0.5, "cache_write": 6.25, "tier": {"type": "context", "size": 272000}}],
				"context_over_200k": {"input": 5, "output": 22.5, "cache_read": 0.5, "cache_write": 6.25}
			}
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
		},
		"gpt-5.6-terra": {
			"id": "gpt-5.6-terra",
			"name": "GPT-5.6 Terra",
			"family": "gpt",
			"last_updated": "2027-01-01",
			"cost": {"input": 9, "output": 99, "cache_read": 0.9, "cache_write": 9.9}
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

func usePricingCatalogTransport(t *testing.T, body string) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = pricingCatalogTransport{body: body}
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func seedPricingUsageModels(t *testing.T, db *gorm.DB, modelNames ...string) {
	t.Helper()
	var events []entities.UsageEvent
	for _, model := range modelNames {
		events = append(events, entities.UsageEvent{EventKey: model, Model: model, Timestamp: time.Unix(1, 0), APIGroupKey: "provider-a"})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}
}
