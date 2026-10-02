package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/pricing"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type pricingStub struct {
	usedModels    []string
	err           error
	recalcOptions servicedto.RecalculationOptions
	recalcStart   *servicedto.StartRecalculationRequest
	recalcReply   servicedto.StartRecalculationResponse
	recalcCurrent *servicedto.RecalculationTask
	recalcErr     error
}

func (s *pricingStub) ListPricingModels(context.Context) (servicedto.PricingModelsResponse, error) {
	return servicedto.PricingModelsResponse{}, s.err
}

func (s *pricingStub) SavePricingModel(context.Context, pricing.ModelPricingConfig) (servicedto.SavePricingModelResponse, error) {
	return servicedto.SavePricingModelResponse{}, s.err
}

func (s *pricingStub) DeletePricingModel(context.Context, string) (servicedto.DeletePricingModelResponse, error) {
	return servicedto.DeletePricingModelResponse{}, s.err
}

func (s *pricingStub) FetchPricingSync(context.Context, string) (servicedto.PricingSyncFetchResponse, error) {
	return servicedto.PricingSyncFetchResponse{}, s.err
}

func (s *pricingStub) ApplyPricingSync(context.Context, servicedto.PricingSyncApplyRequest) (servicedto.PricingSyncApplyResponse, error) {
	return servicedto.PricingSyncApplyResponse{}, s.err
}

func (s *pricingStub) GetPricingRecalculationOptions(context.Context) (servicedto.RecalculationOptions, error) {
	return s.recalcOptions, s.recalcErr
}

func (s *pricingStub) StartPricingRecalculation(_ context.Context, input servicedto.StartRecalculationRequest) (servicedto.StartRecalculationResponse, error) {
	s.recalcStart = &input
	return s.recalcReply, s.recalcErr
}

func (s *pricingStub) CurrentPricingRecalculation(context.Context) (*servicedto.RecalculationTask, error) {
	return s.recalcCurrent, s.recalcErr
}

func (s *pricingStub) WaitPricingRecalculation() {}

func (s *pricingStub) ListUsedModels(context.Context) ([]string, error) {
	return s.usedModels, s.err
}

func TestPricingRouterRegistersOnlyCompleteContract(t *testing.T) {
	router := keeperapi.NewRouter(nil, nil, nil, &pricingStub{}, keeperapi.AuthConfig{}, nil, "")
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/pricing/models", "GET /api/v1/pricing/model-options", "PUT /api/v1/pricing/models", "DELETE /api/v1/pricing/models",
		"GET /api/v1/pricing/sync/fetch", "POST /api/v1/pricing/sync/apply",
		"GET /api/v1/pricing/recalculations/options", "POST /api/v1/pricing/recalculations", "GET /api/v1/pricing/recalculations/current",
	} {
		if !registered[route] {
			t.Errorf("new pricing route %s is missing", route)
		}
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/pricing"}, {http.MethodPut, "/api/v1/pricing"}, {http.MethodDelete, "/api/v1/pricing"},
		{http.MethodPut, "/api/v1/pricing/:model"}, {http.MethodGet, "/api/v1/pricing/rules"}, {http.MethodPut, "/api/v1/pricing/rules"},
		{http.MethodPut, "/api/v1/pricing/batch"}, {http.MethodGet, "/api/v1/pricing/sync/preview"}, {http.MethodGet, "/api/v1/models/used"},
	} {
		if registered[route.method+" "+route.path] {
			t.Errorf("legacy pricing route %s %s is registered", route.method, route.path)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, newPricingRequest(route.method, route.path, ""))
		if response.Code != http.StatusNotFound {
			t.Errorf("legacy pricing request %s %s returned %d, want 404", route.method, route.path, response.Code)
		}
	}
}
