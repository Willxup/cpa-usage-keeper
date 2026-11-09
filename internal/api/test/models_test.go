package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
)

type availableModelsStub struct {
	result *response.ModelsResult
	err    error
	calls  int
}

func (s *availableModelsStub) FetchModels(context.Context) (*response.ModelsResult, error) {
	s.calls++
	return s.result, s.err
}

func TestAvailableModelsRoute(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider *availableModelsStub
		auth     bool
		status   int
		want     string
	}{
		{"catalog", &availableModelsStub{result: &response.ModelsResult{Body: []byte("private upstream payload"), Payload: models.ModelsResponse{Object: "list", Data: []models.ModelInfo{{ID: "custom/model", OwnedBy: "anthropic"}}}}}, false, 200, `"owned_by":"anthropic"`},
		{"empty", &availableModelsStub{result: &response.ModelsResult{}}, false, 200, `"data":[]`},
		{"upstream failure", &availableModelsStub{err: errors.New("secret upstream detail")}, false, 502, "models_unavailable"},
		{"nil result", &availableModelsStub{}, false, 502, "models_unavailable"},
		{"unauthenticated", &availableModelsStub{}, true, 401, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{Enabled: tc.auth}, nil, "/keeper", keeperapi.OptionalProviders{Models: tc.provider})
			req := httptest.NewRequest(http.MethodGet, "/keeper/api/v1/models", nil)
			req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status || !strings.Contains(out.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if strings.Contains(out.Body.String(), "secret") || strings.Contains(out.Body.String(), "private") {
				t.Fatal("leaked upstream details")
			}
			if tc.auth && tc.provider.calls != 0 {
				t.Fatal("unauthenticated request reached CPA")
			}
			if !tc.auth && out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("catalog must not be cached")
			}
		})
	}
}
