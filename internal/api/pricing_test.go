package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type pricingStub struct {
	usedModels  []string
	pricing     []entities.ModelPriceSetting
	preview     servicedto.PricingSyncPreview
	updated     *entities.ModelPriceSetting
	lastUpdate  *servicedto.UpdatePricingInput
	deleted     string
	deletedTier *string
	previewSrc  string
	err         error
}

func (s pricingStub) ListUsedModels(context.Context) ([]string, error) {
	return s.usedModels, s.err
}

func (s pricingStub) ListPricing(context.Context) ([]entities.ModelPriceSetting, error) {
	return s.pricing, s.err
}

func (s *pricingStub) PreviewPricingSync(_ context.Context, source string) (servicedto.PricingSyncPreview, error) {
	s.previewSrc = source
	return s.preview, s.err
}

func (s *pricingStub) UpdatePricing(_ context.Context, input servicedto.UpdatePricingInput) (*entities.ModelPriceSetting, error) {
	s.lastUpdate = &input
	return s.updated, s.err
}

func (s *pricingStub) DeletePricing(_ context.Context, model string, serviceTier *string) error {
	s.deleted = model
	s.deletedTier = serviceTier
	return s.err
}

func TestPricingRoutesReturnEmptyResponsesWithoutProvider(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")

	usedReq := httptest.NewRequest(http.MethodGet, "/api/v1/models/used", nil)
	usedResp := httptest.NewRecorder()
	router.ServeHTTP(usedResp, usedReq)
	if usedResp.Code != http.StatusOK || !contains(usedResp.Body.String(), `"models":[]`) {
		t.Fatalf("unexpected used models response: %d %s", usedResp.Code, usedResp.Body.String())
	}

	pricingReq := httptest.NewRequest(http.MethodGet, "/api/v1/pricing", nil)
	pricingResp := httptest.NewRecorder()
	router.ServeHTTP(pricingResp, pricingReq)
	if pricingResp.Code != http.StatusOK || !contains(pricingResp.Body.String(), `"pricing":[]`) {
		t.Fatalf("unexpected pricing response: %d %s", pricingResp.Code, pricingResp.Body.String())
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/sync/preview", nil)
	previewResp := httptest.NewRecorder()
	router.ServeHTTP(previewResp, previewReq)
	if previewResp.Code != http.StatusOK || !contains(previewResp.Body.String(), `"matches":[]`) {
		t.Fatalf("unexpected pricing sync preview response: %d %s", previewResp.Code, previewResp.Body.String())
	}
}

func TestPricingRoutesReturnConfiguredData(t *testing.T) {
	router := NewRouter(nil, nil, nil, &pricingStub{
		usedModels: []string{"claude-sonnet"},
		pricing: []entities.ModelPriceSetting{{
			Model:                   "claude-sonnet",
			ServiceTier:             "priority",
			PricingStyle:            "claude",
			PromptPricePer1M:        3,
			CompletionPricePer1M:    15,
			CachePricePer1M:         0.3,
			CacheCreationPricePer1M: 3.75,
		}},
	}, AuthConfig{}, nil, "")

	usedReq := httptest.NewRequest(http.MethodGet, "/api/v1/models/used", nil)
	usedResp := httptest.NewRecorder()
	router.ServeHTTP(usedResp, usedReq)
	if usedResp.Code != http.StatusOK || !contains(usedResp.Body.String(), `claude-sonnet`) {
		t.Fatalf("unexpected used models response: %d %s", usedResp.Code, usedResp.Body.String())
	}

	pricingReq := httptest.NewRequest(http.MethodGet, "/api/v1/pricing", nil)
	pricingResp := httptest.NewRecorder()
	router.ServeHTTP(pricingResp, pricingReq)
	if pricingResp.Code != http.StatusOK || !contains(pricingResp.Body.String(), `"prompt_price_per_1m":3`) || !contains(pricingResp.Body.String(), `"pricing_style":"claude"`) || !contains(pricingResp.Body.String(), `"service_tier":"priority"`) || !contains(pricingResp.Body.String(), `"cache_creation_price_per_1m":3.75`) {
		t.Fatalf("unexpected pricing response: %d %s", pricingResp.Code, pricingResp.Body.String())
	}
}

func TestPricingSyncPreviewRoute(t *testing.T) {
	provider := &pricingStub{
		preview: servicedto.PricingSyncPreview{
			SourceID:       "openai_official",
			Source:         "Models.dev",
			MetadataModels: 1,
			Matches: []servicedto.PricingSyncMatch{{
				Model:                "openai/gpt-4o",
				ServiceTier:          "priority",
				MatchedModel:         "gpt-4o",
				MatchType:            "suffix",
				PricingStyle:         "openai",
				PromptPricePer1M:     2.5,
				CompletionPricePer1M: 10,
				CachePricePer1M:      1.25,
			}},
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/sync/preview?source=openai_official", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"source_id":"openai_official"`) || !contains(resp.Body.String(), `"service_tier":"priority"`) || !contains(resp.Body.String(), `"matched_model":"gpt-4o"`) {
		t.Fatalf("unexpected preview response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.previewSrc != "openai_official" {
		t.Fatalf("expected preview source to pass through, got %q", provider.previewSrc)
	}
}

func TestUpdatePricingRoute(t *testing.T) {
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                   "claude-sonnet",
			ServiceTier:             "priority",
			PricingStyle:            "claude",
			PromptPricePer1M:        3,
			CompletionPricePer1M:    15,
			CachePricePer1M:         0.3,
			CacheCreationPricePer1M: 3.75,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/pricing/claude-sonnet", strings.NewReader(`{"service_tier":"priority","pricing_style":"claude","prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_price_per_1m":0.3,"cache_creation_price_per_1m":3.75}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"claude-sonnet"`) || !contains(resp.Body.String(), `"service_tier":"priority"`) || !contains(resp.Body.String(), `"pricing_style":"claude"`) {
		t.Fatalf("unexpected update response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.PricingStyle != "claude" || provider.lastUpdate.ServiceTier != "priority" || provider.lastUpdate.CacheCreationPricePer1M != 3.75 {
		t.Fatalf("expected Claude pricing fields to pass through, got %+v", provider.lastUpdate)
	}
}

func TestUpdatePricingRouteAcceptsModelInBody(t *testing.T) {
	provider := &pricingStub{
		updated: &entities.ModelPriceSetting{
			Model:                "openai/gpt-4.1",
			ServiceTier:          "default",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CachePricePer1M:      0.3,
		},
	}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/pricing", strings.NewReader(`{"model":"openai/gpt-4.1","service_tier":"default","prompt_price_per_1m":3,"completion_price_per_1m":15,"cache_price_per_1m":0.3}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"model":"openai/gpt-4.1"`) {
		t.Fatalf("unexpected update response: %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastUpdate == nil || provider.lastUpdate.Model != "openai/gpt-4.1" || provider.lastUpdate.ServiceTier != "default" {
		t.Fatalf("expected model from body to be passed through, got %+v", provider.lastUpdate)
	}
}

func TestDeletePricingRoute(t *testing.T) {
	provider := &pricingStub{}
	router := NewRouter(nil, nil, nil, provider, AuthConfig{}, nil, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pricing?model=openai%2Fgpt-4.1&service_tier=priority", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.deleted != "openai/gpt-4.1" {
		t.Fatalf("expected model to be deleted, got %q", provider.deleted)
	}
	if provider.deletedTier == nil || *provider.deletedTier != "priority" {
		t.Fatalf("expected service tier to be deleted, got %+v", provider.deletedTier)
	}
}
