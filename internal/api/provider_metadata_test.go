package api

import (
	"context"
	"errors"
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
	router := NewRouter(nil, nil, nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-metadata", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"items":[]`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestProviderMetadataRouteReturnsStoredMetadata(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, providerMetadataStub{items: []models.ProviderMetadata{{
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
	if contains(body, `sk-test-1234`) {
		t.Fatalf("expected raw lookup key to be redacted from response body: %s", body)
	}
	if !(contains(body, `"lookup_key":"openai:ChatGPT Mirror"`) && contains(body, `"display_name":"ChatGPT Mirror"`) && contains(body, `"provider_key":"openai:ChatGPT Mirror"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestProviderMetadataRouteHidesInternalErrors(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, providerMetadataStub{err: errors.New("database contains sk-secret-1234")}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-metadata", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}
	if contains(body, "sk-secret-1234") || contains(body, "database contains") {
		t.Fatalf("expected internal error details to be hidden, got %s", body)
	}
	if !contains(body, `"error":"internal server error"`) {
		t.Fatalf("expected stable internal error response, got %s", body)
	}
}

func TestProviderMetadataRouteDisambiguatesSameNamedProviders(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, providerMetadataStub{items: []models.ProviderMetadata{
		{ID: 1, LookupKey: "sk-test-1234", ProviderType: "openai", DisplayName: "Shared"},
		{ID: 2, LookupKey: "sk-test-5678", ProviderType: "openai", DisplayName: "Shared"},
	}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/provider-metadata", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if !contains(body, `"lookup_key":"provider:1"`) || !contains(body, `"lookup_key":"provider:2"`) {
		t.Fatalf("expected provider ids to disambiguate same display names, got %s", body)
	}
	if contains(body, "sk-test-1234") || contains(body, "sk-test-5678") {
		t.Fatalf("expected raw lookup keys to be redacted, got %s", body)
	}
}
