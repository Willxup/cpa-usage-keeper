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
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type pricingReviewTransport func(*http.Request) (*http.Response, error)

func (f pricingReviewTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func previewReviewCatalog(t *testing.T, source, catalog string, names ...string) servicedto.PricingSyncPreview {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = pricingReviewTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(catalog))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	modelList := make([]models.ModelInfo, 0, len(names))
	for _, name := range names {
		modelList = append(modelList, models.ModelInfo{ID: name})
	}
	provider := service.NewPricingService(openPricingServiceTestDatabase(t), emptyPricingCatalogForTest(),
		stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: modelList}}})
	preview, err := provider.PreviewPricingSync(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestPricingSyncSeparatesFineTunedModelIdentity(t *testing.T) {
	catalog := `{
		"gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.0000005,"output_cost_per_token":0.0000015},
		"ft:gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000006}
	}`
	want := map[string]string{
		"gpt-3.5-turbo":           "gpt-3.5-turbo",
		"custom/gpt-3.5-turbo":    "gpt-3.5-turbo",
		"ft:gpt-3.5-turbo":        "ft:gpt-3.5-turbo",
		"custom/ft:gpt-3.5-turbo": "ft:gpt-3.5-turbo",
		"custom:ft:gpt-3.5-turbo": "ft:gpt-3.5-turbo",
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	preview := previewReviewCatalog(t, "litellm", catalog, names...)
	if len(preview.Matches) != len(want) {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	for _, match := range preview.Matches {
		if match.MatchedModel != want[match.Model] {
			t.Errorf("%s matched %s, want %s", match.Model, match.MatchedModel, want[match.Model])
		}
		price := 0.5
		if strings.HasPrefix(want[match.Model], "ft:") {
			price = 3
		}
		if math.Abs(match.PromptPricePer1M-price) > 1e-10 {
			t.Errorf("%s input price=%v, want %v", match.Model, match.PromptPricePer1M, price)
		}
	}
}

func TestPricingSyncDoesNotFallbackAcrossFineTuningIdentity(t *testing.T) {
	for _, tc := range []struct{ name, catalog string }{
		{"gpt-3.5-turbo", `{"ft:gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000006}}`},
		{"ft:gpt-3.5-turbo", `{"gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.0000005,"output_cost_per_token":0.0000015}}`},
		{"ft:gpt-3.5-turbo:org:suffix:id", `{"gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":0.0000005,"output_cost_per_token":0.0000015},"id":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":1,"output_cost_per_token":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preview := previewReviewCatalog(t, "litellm", tc.catalog, tc.name)
			if len(preview.Matches) != 0 || len(preview.UnmatchedModels) != 1 {
				t.Fatalf("must not cross billing identities: %+v", preview)
			}
		})
	}
}

func TestPricingSyncPrefersLiteLLMOfficialProviderAliases(t *testing.T) {
	for _, tc := range []struct {
		provider, model, officialID string
		input, output               float64
	}{
		{"moonshot", "kimi-k2.5", "moonshotai", 0.6, 3},
		{"dashscope", "qwen-plus", "alibaba-cn", 0.4, 1.2},
		{"qwencloud", "qwen-plus", "alibaba", 0.4, 1.2},
		{"qwen_ai_platform", "qwen-plus", "alibaba-cn", 0.4, 1.2},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			catalog := fmt.Sprintf(`{
				%q:{"litellm_provider":%q,"mode":"chat","input_cost_per_token":%g,"output_cost_per_token":%g},
				%q:{"litellm_provider":"openrouter","mode":"chat","input_cost_per_token":0.00000026,"output_cost_per_token":0.00000078}
			}`, tc.provider+"/"+tc.model, tc.provider, tc.input/1e6, tc.output/1e6, "openrouter/"+tc.model)
			preview := previewReviewCatalog(t, "litellm", catalog, tc.model, "custom/"+tc.model)
			if len(preview.Matches) != 2 {
				t.Fatalf("unexpected preview: %+v", preview)
			}
			for _, match := range preview.Matches {
				if match.SourceProviderID != tc.officialID || math.Abs(match.PromptPricePer1M-tc.input) > 1e-10 || math.Abs(match.CompletionPricePer1M-tc.output) > 1e-10 {
					t.Errorf("expected official %s pricing: %+v", tc.provider, match)
				}
			}
		})
	}
}

func TestPricingSyncPrefersOpenAITextCompletionPrices(t *testing.T) {
	for _, tc := range []struct{ name, input, provider, matchedModel string }{
		{"official", "0.0000015", "openai", "gpt-3.5-turbo-instruct"},
		{"invalid_official_fallback", "-1", "openrouter", "openrouter/openai/gpt-3.5-turbo-instruct"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := fmt.Sprintf(`{
				"gpt-3.5-turbo-instruct":{"litellm_provider":"text-completion-openai","mode":"completion","input_cost_per_token":%s,"output_cost_per_token":0.000002},
				"openrouter/openai/gpt-3.5-turbo-instruct":{"litellm_provider":"openrouter","mode":"chat","input_cost_per_token":0.0000015,"output_cost_per_token":0.000002}
			}`, tc.input)
			preview := previewReviewCatalog(t, "litellm", catalog,
				"gpt-3.5-turbo-instruct", "custom/gpt-3.5-turbo-instruct", "custom:gpt-3.5-turbo-instruct")
			if len(preview.Matches) != 3 {
				t.Fatalf("unexpected preview: %+v", preview)
			}
			for _, match := range preview.Matches {
				if match.SourceProviderID != tc.provider || match.MatchedModel != tc.matchedModel ||
					math.Abs(match.PromptPricePer1M-1.5) > 1e-10 || math.Abs(match.CompletionPricePer1M-2) > 1e-10 {
					t.Errorf("expected %s text completion pricing: %+v", tc.provider, match)
				}
			}
		})
	}
}

func TestPricingSyncOfficialPriceBeforeMatchFormatting(t *testing.T) {
	for _, source := range []string{"models-dev", "litellm"} {
		for _, tc := range []struct {
			name, provider, officialModel, input, output, cache string
			wantProvider                                        string
			wantInput                                           float64
		}{
			{"normalized_official", "openai", "gpt-4.1", "2", "8", "null", "openai", 2},
			{"missing_official_model", "openai", "gpt-other", "2", "8", "null", "openrouter", 9},
			{"missing_official_input", "openai", "gpt-4.1", "null", "8", "null", "openrouter", 9},
			{"invalid_official_output", "openai", "gpt-4.1", "2", "-1", "null", "openrouter", 9},
			{"invalid_official_cache", "openai", "gpt-4.1", "2", "8", "-1", "openrouter", 9},
			{"explicit_official_zero", "openai", "gpt-4.1", "0", "0", "null", "openai", 0},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				catalog := reviewOfficialPriceCatalog(source, tc.provider, tc.officialModel, "openrouter", "gpt_4_1", tc.input, tc.output, tc.cache)
				preview := previewReviewCatalog(t, source, catalog, "gpt_4_1", "custom/gpt_4_1", "missing-model")
				if len(preview.Matches) != 2 || len(preview.UnmatchedModels) != 1 || preview.UnmatchedModels[0] != "missing-model" {
					t.Fatalf("unexpected preview: %+v", preview)
				}
				for _, match := range preview.Matches {
					if match.SourceProviderID != tc.wantProvider || math.Abs(match.PromptPricePer1M-tc.wantInput) > 1e-10 {
						t.Errorf("expected %s pricing: %+v", tc.wantProvider, match)
					}
					if match.CacheReadPricePer1M != 0 || match.CacheWritePricePer1M != 0 {
						t.Errorf("missing cache prices must remain zero: %+v", match)
					}
				}
			})
		}
	}
}

func TestPricingSyncPreservesProviderFallbackOrder(t *testing.T) {
	for _, source := range []string{"models-dev", "litellm"} {
		for _, tc := range []struct {
			name, provider, officialModel, fallback, model, input, output, requested, want string
		}{
			{"native_before_cloud", "openai", "gpt-4.1", "azure", "gpt_4_1", "2", "8", "gpt_4_1", "openai"},
			{"cloud_when_native_unavailable", "openai", "gpt-4.1", "azure", "gpt_4_1", "null", "8", "gpt_4_1", "azure"},
			{"third_party_exact_before_normalized", "openrouter", "gpt-4.1", "custom", "gpt_4_1", "2", "8", "gpt_4_1", "custom"},
			{"plan_zero_does_not_replace_api_price", "minimax-coding-plan", "minimax-m3", "openrouter", "minimax_m3", "0", "0", "minimax_m3", "openrouter"},
			{"exact_plan_zero_does_not_replace_api_price", "minimax-coding-plan", "minimax_m3", "openrouter", "minimax-m3", "0", "0", "minimax_m3", "openrouter"},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				catalog := reviewOfficialPriceCatalog(source, tc.provider, tc.officialModel, tc.fallback, tc.model, tc.input, tc.output, "null")
				preview := previewReviewCatalog(t, source, catalog, "custom/"+tc.requested)
				if len(preview.Matches) != 1 || preview.Matches[0].SourceProviderID != tc.want {
					t.Fatalf("expected %s pricing: %+v", tc.want, preview)
				}
			})
		}
	}
}

func reviewOfficialPriceCatalog(source, provider, model, fallback, fallbackModel, input, output, cache string) string {
	if source == "models-dev" {
		return fmt.Sprintf(`{
			%q:{"models":{%q:{"cost":{"input":%s,"output":%s,"cache_read":%s}}}},
			%q:{"models":{%q:{"cost":{"input":9,"output":18}}}}
		}`, provider, model, input, output, cache, fallback, fallbackModel)
	}
	// 测试夹具以美元/百万 token 描述期望；LiteLLM 的原始 JSON 使用每 token 单价。
	perToken := func(value string) string {
		if value == "null" {
			return value
		}
		return value + "e-6"
	}
	return fmt.Sprintf(`{
		%q:{"litellm_provider":%q,"mode":"chat","input_cost_per_token":%s,"output_cost_per_token":%s,"cache_read_input_token_cost":%s},
		%q:{"litellm_provider":%q,"mode":"chat","input_cost_per_token":0.000009,"output_cost_per_token":0.000018}
	}`, provider+"/"+model, provider, perToken(input), perToken(output), perToken(cache), fallback+"/"+fallbackModel, fallback)
}
