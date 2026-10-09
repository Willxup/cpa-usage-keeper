package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type pricingStub struct {
	usedModels    []string
	pricing       []entities.ModelPriceSetting
	preview       servicedto.PricingSyncPreview
	previewSource *string
	updated       *entities.ModelPriceSetting
	lastUpdate    *servicedto.UpdatePricingInput
	batch         []entities.ModelPriceSetting
	lastBatch     []servicedto.UpdatePricingInput
	rules         []servicedto.PricingRule
	lastRules     *servicedto.ReplacePricingRulesInput
	deleted       string
	err           error
}

type pricingTimeoutError struct{}

func (pricingTimeoutError) Error() string   { return "net/http: TLS handshake timeout" }
func (pricingTimeoutError) Timeout() bool   { return true }
func (pricingTimeoutError) Temporary() bool { return true }

func (s pricingStub) ListUsedModels(context.Context) ([]string, error) {
	return s.usedModels, s.err
}

func (s pricingStub) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	return s.pricing, s.err
}

func (s pricingStub) PreviewPricingSync(_ context.Context, source string) (servicedto.PricingSyncPreview, error) {
	if s.previewSource != nil {
		*s.previewSource = source
	}
	return s.preview, s.err
}

func TestPricingSyncPreviewSelectsSource(t *testing.T) {
	for _, tc := range []struct {
		query, source string
		status        int
	}{
		{"", "models-dev", http.StatusOK},
		{"?source=litellm", "litellm", http.StatusOK},
		{"?source=other", "", http.StatusBadRequest},
	} {
		t.Run(tc.query, func(t *testing.T) {
			var source string
			router := NewRouter(nil, nil, nil, &pricingStub{previewSource: &source}, AuthConfig{}, nil, "")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/pricing/sync/preview"+tc.query, nil))
			if resp.Code != tc.status || source != tc.source {
				t.Fatalf("status=%d source=%q, want %+v", resp.Code, source, tc)
			}
		})
	}
}

func TestPricingSyncLiteLLMTimeoutNamesSelectedSource(t *testing.T) {
	router := NewRouter(nil, nil, nil, &pricingStub{err: context.DeadlineExceeded}, AuthConfig{}, nil, "")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/pricing/sync/preview?source=litellm", nil))
	if resp.Code != http.StatusGatewayTimeout || !strings.Contains(resp.Body.String(), "LiteLLM request timed out") {
		t.Fatalf("unexpected timeout: %d %s", resp.Code, resp.Body.String())
	}
}

func (s *pricingStub) UpdatePricing(_ context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	s.lastUpdate = &input
	return s.updated, s.err
}

func (s *pricingStub) UpdatePricingBatch(_ context.Context, input []servicedto.UpdatePricingInput) ([]entities.ModelPriceSetting, error) {
	s.lastBatch = input
	return s.batch, s.err
}

func (s *pricingStub) DeletePricing(_ context.Context, model string) error {
	s.deleted = model
	return s.err
}

func (s *pricingStub) ListPricingRules(context.Context, string) ([]servicedto.PricingRule, error) {
	return s.rules, s.err
}

func (s *pricingStub) ReplacePricingRules(_ context.Context, input servicedto.ReplacePricingRulesInput) ([]servicedto.PricingRule, error) {
	s.lastRules = &input
	return s.rules, s.err
}

func TestPricingRoutesReturnEmptyResponsesWithoutProvider(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	for path, want := range map[string]string{
		"/api/v1/models/used":          `"models":[]`,
		"/api/v1/pricing":              `"pricing":[]`,
		"/api/v1/pricing/sync/preview": `"matches":[]`,
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodGet, path, ""))
			if response.Code != http.StatusOK || !contains(response.Body.String(), want) {
				t.Fatalf("unexpected response: %d %s; want %s", response.Code, response.Body.String(), want)
			}
		})
	}
}

func TestPricingRoutesReturnConfiguredData(t *testing.T) {
	multiplier := 0.5
	router := NewRouter(nil, nil, nil, &pricingStub{
		usedModels: []string{"claude-sonnet"},
		pricing: []entities.ModelPriceSetting{{
			Model:                "claude-sonnet",
			PricingStyle:         "claude",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CacheReadPricePer1M:  0.3,
			CacheWritePricePer1M: 3.75,
			PriceMultiplier:      &multiplier,
		}},
	}, AuthConfig{}, nil, "")

	usedReq := newPricingRequest(http.MethodGet, "/api/v1/models/used", "")
	usedResp := httptest.NewRecorder()
	router.ServeHTTP(usedResp, usedReq)
	if usedResp.Code != http.StatusOK || !contains(usedResp.Body.String(), `claude-sonnet`) {
		t.Fatalf("unexpected used models response: %d %s", usedResp.Code, usedResp.Body.String())
	}

	pricingReq := newPricingRequest(http.MethodGet, "/api/v1/pricing", "")
	pricingResp := httptest.NewRecorder()
	router.ServeHTTP(pricingResp, pricingReq)
	if pricingResp.Code != http.StatusOK || !contains(pricingResp.Body.String(), `"prompt_price_per_1m":3`) || !contains(pricingResp.Body.String(), `"pricing_style":"claude"`) || !contains(pricingResp.Body.String(), `"cache_write_price_per_1m":3.75`) || !contains(pricingResp.Body.String(), `"price_multiplier":0.5`) {
		t.Fatalf("unexpected pricing response: %d %s", pricingResp.Code, pricingResp.Body.String())
	}
}

func TestPricingSyncPreviewRoute(t *testing.T) {
	router := NewRouter(nil, nil, nil, &pricingStub{
		preview: servicedto.PricingSyncPreview{
			Source:         "Models.dev",
			MetadataModels: 1,
			Matches: []servicedto.PricingSyncMatch{{
				Model:                "gpt-5.6-terra",
				MatchedModel:         "gpt-5.6-terra",
				MatchType:            "index_exact",
				SourceProviderID:     "openai",
				SourceProviderName:   "OpenAI",
				PricingStyle:         "openai",
				PromptPricePer1M:     2.5,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.25,
				CacheWritePricePer1M: 3.125,
			}},
		},
	}, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodGet, "/api/v1/pricing/sync/preview", "")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK ||
		!contains(resp.Body.String(), `"source":"Models.dev"`) ||
		!contains(resp.Body.String(), `"matched_model":"gpt-5.6-terra"`) ||
		!contains(resp.Body.String(), `"source_provider_id":"openai"`) ||
		!contains(resp.Body.String(), `"pricing_style":"openai"`) ||
		!contains(resp.Body.String(), `"cache_read_price_per_1m":0.25`) ||
		!contains(resp.Body.String(), `"cache_write_price_per_1m":3.125`) {
		t.Fatalf("unexpected preview response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestPricingSyncPreviewRouteMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"deadline", fmt.Errorf("fetch pricing catalog: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, `"error":"Models.dev request timed out"`},
		{"network timeout", fmt.Errorf("fetch pricing catalog: %w", pricingTimeoutError{}), http.StatusGatewayTimeout, `"error":"Models.dev request timed out"`},
		{"internal", errors.New("decode pricing catalog: invalid character"), http.StatusInternalServerError, `"error":"internal server error"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(nil, nil, nil, &pricingStub{err: tc.err}, AuthConfig{}, nil, "")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodGet, "/api/v1/pricing/sync/preview", ""))
			if response.Code != tc.status || !contains(response.Body.String(), tc.message) {
				t.Fatalf("unexpected response: %d %s; want %d %s", response.Code, response.Body.String(), tc.status, tc.message)
			}
		})
	}
}

func TestUpdatePricingRoutePreservesOpenAICacheWritePrice(t *testing.T) {
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                "gpt-5.6-terra",
			PricingStyle:         "openai",
			PromptPricePer1M:     2.5,
			CompletionPricePer1M: 15,
			CacheReadPricePer1M:  0.25,
			CacheWritePricePer1M: 3.125,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/gpt-5.6-terra", `{"pricing_style":"openai","prompt_price_per_1m":2.5,"completion_price_per_1m":15,"cache_read_price_per_1m":0.25,"cache_write_price_per_1m":3.125}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"cache_write_price_per_1m":3.125`) {
		t.Fatalf("unexpected OpenAI pricing response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.PricingStyle != "openai" || provider.lastUpdate.CacheReadPricePer1M != 0.25 || provider.lastUpdate.CacheWritePricePer1M != 3.125 {
		t.Fatalf("expected OpenAI cache prices to pass through, got %+v", provider.lastUpdate)
	}
}

func TestUpdatePricingRouteRejectsLegacyCachePriceFields(t *testing.T) {
	provider := &pricingStub{}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/gpt-5.6-terra", `{"cache_price_per_1m":0.25,"cache_creation_price_per_1m":3.125}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected legacy pricing fields to be rejected, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate != nil {
		t.Fatalf("expected rejected legacy pricing request not to update provider, got %+v", provider.lastUpdate)
	}
}

func TestUpdatePricingRoute(t *testing.T) {
	multiplier := 0.5
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                "claude-sonnet",
			PricingStyle:         "claude",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CacheReadPricePer1M:  0.3,
			CacheWritePricePer1M: 3.75,
			PriceMultiplier:      &multiplier,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/claude-sonnet", `{"pricing_style":"claude","prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_read_price_per_1m":0.3,"cache_write_price_per_1m":3.75,"price_multiplier":0.5}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"claude-sonnet"`) || !contains(resp.Body.String(), `"pricing_style":"claude"`) || !contains(resp.Body.String(), `"price_multiplier":0.5`) {
		t.Fatalf("unexpected update response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.PricingStyle != "claude" || provider.lastUpdate.CacheWritePricePer1M != 3.75 || provider.lastUpdate.PriceMultiplier == nil || *provider.lastUpdate.PriceMultiplier != 0.5 {
		t.Fatalf("expected Claude pricing fields to pass through, got %+v", provider.lastUpdate)
	}
}

func TestBatchUpdatePricingRouteUsesOneProviderCall(t *testing.T) {
	provider := &pricingStub{batch: []entities.ModelPriceSetting{
		{Model: "model-a", PromptPricePer1M: 2},
		{Model: "model-b", PromptPricePer1M: 3},
	}}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/batch", `{"pricing":[{"model":"model-a","prompt_price_per_1m":2},{"model":"model-b","prompt_price_per_1m":3}]}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"model-a"`) || !contains(resp.Body.String(), `"model":"model-b"`) {
		t.Fatalf("unexpected batch response: %d %s", resp.Code, resp.Body.String())
	}
	if len(provider.lastBatch) != 2 || provider.lastBatch[0].Model != "model-a" || provider.lastBatch[1].Model != "model-b" {
		t.Fatalf("expected one two-model batch call, got %+v", provider.lastBatch)
	}
	if provider.lastUpdate != nil {
		t.Fatalf("batch route must not call single update, got %+v", provider.lastUpdate)
	}
}

func TestBatchUpdatePricingRouteMapsInvalidInputToBadRequest(t *testing.T) {
	provider := &pricingStub{err: service.ErrInvalidPricingInput}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")
	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/batch", `{"pricing":[{"model":"overflow-model","prompt_price_per_1m":1.7976931348623157e308}]}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid batch input to map to 400, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUpdatePricingRouteAllowsZeroPriceMultiplier(t *testing.T) {
	zero := 0.0
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                "free-model",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CacheReadPricePer1M:  0.3,
			PriceMultiplier:      &zero,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/free-model", `{"prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_read_price_per_1m":0.3,"price_multiplier":0}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"price_multiplier":0`) {
		t.Fatalf("unexpected zero multiplier response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.PriceMultiplier == nil || *provider.lastUpdate.PriceMultiplier != 0 {
		t.Fatalf("expected zero price multiplier to pass through, got %+v", provider.lastUpdate)
	}
}

func TestUpdatePricingRouteMapsPriceMultiplierValidationToBadRequest(t *testing.T) {
	provider := &pricingStub{err: errors.Join(service.ErrInvalidPricingInput, errors.New("price_multiplier must be non-negative"))}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/free-model", `{"prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_read_price_per_1m":0.3,"price_multiplier":-1}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest || !contains(resp.Body.String(), "price_multiplier must be non-negative") {
		t.Fatalf("expected price multiplier validation to map to 400, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUpdatePricingRouteMapsInvalidSnapshotInputToBadRequest(t *testing.T) {
	provider := &pricingStub{err: service.ErrInvalidPricingInput}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodPut, "/api/v1/pricing/overflow-model", `{"prompt_price_per_1m":1.7976931348623157e308}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe price input to map to 400, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUpdatePricingRouteAcceptsModelInBody(t *testing.T) {
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                "openai/gpt-4.1",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CacheReadPricePer1M:  0.3,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodPut, "/api/v1/pricing", `{"model":"openai/gpt-4.1","prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_read_price_per_1m":0.3}`)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"openai/gpt-4.1"`) {
		t.Fatalf("unexpected update response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.Model != "openai/gpt-4.1" {
		t.Fatalf("expected model from body to be passed through, got %+v", provider.lastUpdate)
	}
}

func TestDeletePricingRoute(t *testing.T) {
	provider := &pricingStub{}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := newPricingRequest(http.MethodDelete, "/api/v1/pricing?model=openai%2Fgpt-4.1", "")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.deleted != "openai/gpt-4.1" {
		t.Fatalf("expected model to be deleted, got %q", provider.deleted)
	}
}
