package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/models"
)

type providerMetadataStub struct {
	items []models.ProviderMetadata
	err   error
}

func (s providerMetadataStub) ListProviderMetadata(context.Context) ([]models.ProviderMetadata, error) {
	return s.items, s.err
}

func TestProviderMetadataRouteReturnsEmptyResponseWithoutProvider(t *testing.T) {
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-metadata", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"items":[]`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestProviderMetadataRouteReturnsStoredMetadata(t *testing.T) {
	router := NewRouter("", nil, nil, nil, providerMetadataStub{items: []models.ProviderMetadata{{
		LookupKey:    "sk-test-1234",
		ProviderType: "openai",
		DisplayName:  "ChatGPT Mirror",
		ProviderKey:  "openai:ChatGPT Mirror",
	}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-metadata", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"lookup_key":"sk-test-1234"`) && contains(body, `"display_name":"ChatGPT Mirror"`) && contains(body, `"provider_key":"openai:ChatGPT Mirror"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}
