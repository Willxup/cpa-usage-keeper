package test

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service"
)

type liteLLMCatalogTransport struct{ t *testing.T }

func (transport liteLLMCatalogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.String() != "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json" {
		transport.t.Fatalf("unexpected pricing source: %s", request.URL)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
		"gpt-test":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.0000025,"output_cost_per_token":0.000015,"cache_read_input_token_cost":0.00000025},
		"azure/gpt-test":{"litellm_provider":"azure","mode":"chat","input_cost_per_token":0.000099,"output_cost_per_token":0.000099},
		"claude-test":{"litellm_provider":"anthropic","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015,"cache_creation_input_token_cost":0.00000375},
		"overflow":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":1e308,"output_cost_per_token":1}
	}`))}, nil
}

func TestPricingSyncLiteLLMUsesSharedMatchingAndZeroCacheDefaults(t *testing.T) {
	transport := http.DefaultTransport
	http.DefaultTransport = liteLLMCatalogTransport{t}
	t.Cleanup(func() { http.DefaultTransport = transport })
	provider := service.NewPricingService(openPricingServiceTestDatabase(t), emptyPricingCatalogForTest(), stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{
		{ID: "custom/gpt-test"}, {ID: "claude-test"}, {ID: "overflow"},
	}}}})
	preview, err := provider.PreviewPricingSync(context.Background(), "litellm")
	if err != nil {
		t.Fatal(err)
	}
	if preview.SourceID != "litellm" || preview.Source != "LiteLLM" || len(preview.Matches) != 2 || len(preview.UnmatchedModels) != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	for _, match := range preview.Matches {
		if match.Model == "custom/gpt-test" {
			if match.SourceProviderID != "openai" || math.Abs(match.PromptPricePer1M-2.5) > 1e-10 || match.CacheWritePricePer1M != 0 {
				t.Errorf("unexpected OpenAI match: %+v", match)
			}
		} else if match.PricingStyle != "claude" || math.Abs(match.CacheWritePricePer1M-3.75) > 1e-10 || match.CacheReadPricePer1M != 0 {
			t.Errorf("unexpected Claude match: %+v", match)
		}
	}
	prices, err := provider.ListPricing(context.Background())
	if err != nil || len(prices) != 0 {
		t.Fatalf("preview must not save prices: %+v, %v", prices, err)
	}
}

func TestPricingSyncLiteLLMCacheHitPriceFallback(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		cacheRead    float64
		invalid      bool
	}{
		{"legacy_only", `,"input_cost_per_token_cache_hit":0.000000028`, 0.028, false},
		{"standard_only", `,"cache_read_input_token_cost":0.00000002`, 0.02, false},
		{"standard_before_legacy", `,"cache_read_input_token_cost":0.00000002,"input_cost_per_token_cache_hit":0.000000028`, 0.02, false},
		{"explicit_standard_zero", `,"cache_read_input_token_cost":0,"input_cost_per_token_cache_hit":0.000000028`, 0, false},
		{"null_standard", `,"cache_read_input_token_cost":null,"input_cost_per_token_cache_hit":0.000000028`, 0.028, false},
		{"explicit_legacy_zero", `,"input_cost_per_token_cache_hit":0`, 0, false},
		{"both_missing", ``, 0, false},
		{"invalid_legacy", `,"input_cost_per_token_cache_hit":-1`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := fmt.Sprintf(`{
				"deepseek/deepseek-v3.2":{"litellm_provider":"deepseek","mode":"chat","input_cost_per_token":0.00000028,"output_cost_per_token":0.0000004%s}
			}`, tc.fields)
			preview := previewReviewCatalog(t, "litellm", catalog, "deepseek-v3.2", "custom/deepseek-v3.2")
			if tc.invalid {
				if len(preview.Matches) != 0 || len(preview.UnmatchedModels) != 2 {
					t.Fatalf("invalid cache price must not produce a match: %+v", preview)
				}
				return
			}
			if len(preview.Matches) != 2 {
				t.Fatalf("unexpected preview: %+v", preview)
			}
			for _, match := range preview.Matches {
				if match.SourceProviderID != "deepseek" || math.Abs(match.CacheReadPricePer1M-tc.cacheRead) > 1e-10 {
					t.Errorf("expected cache read price %v: %+v", tc.cacheRead, match)
				}
				if math.Abs(match.PromptPricePer1M-0.28) > 1e-10 || math.Abs(match.CompletionPricePer1M-0.4) > 1e-10 || match.CacheWritePricePer1M != 0 {
					t.Errorf("unexpected other base prices: %+v", match)
				}
			}
		})
	}
}
