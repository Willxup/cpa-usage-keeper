package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
)

type usageIdentitiesStub struct {
	items []models.UsageIdentity
	err   error
}

func (s usageIdentitiesStub) ListUsageIdentities(context.Context) ([]models.UsageIdentity, error) {
	return s.items, s.err
}

func TestUsageIdentitiesRouteReturnsMetadataStatsAndDeletedRows(t *testing.T) {
	firstUsedAt := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	lastUsedAt := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	statsUpdatedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	deletedAt := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)

	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "", usageIdentitiesStub{items: []models.UsageIdentity{
		{
			ID:                         1,
			Name:                       "Claude Desktop",
			AuthType:                   models.UsageIdentityAuthTypeAuthFile,
			AuthTypeName:               "oauth",
			Identity:                   "2",
			Type:                       "auth-file",
			Provider:                   "anthropic",
			TotalRequests:              10,
			SuccessCount:               8,
			FailureCount:               2,
			InputTokens:                100,
			OutputTokens:               200,
			ReasoningTokens:            30,
			CachedTokens:               40,
			TotalTokens:                370,
			LastAggregatedUsageEventID: 99,
			FirstUsedAt:                &firstUsedAt,
			LastUsedAt:                 &lastUsedAt,
			StatsUpdatedAt:             &statsUpdatedAt,
			CreatedAt:                  createdAt,
			UpdatedAt:                  updatedAt,
		},
		{
			ID:           2,
			Name:         "Deleted Provider",
			AuthType:     models.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     "sk-deleted-provider-secret",
			Type:         "openai",
			Provider:     "OpenAI",
			IsDeleted:    true,
			DeletedAt:    &deletedAt,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"identities":[`) || !contains(body, `"id":1`) || !contains(body, `"identity":"2"`) {
		t.Fatalf("expected auth file identity row in response, got %s", body)
	}
	for _, expected := range []string{
		`"name":"Claude Desktop"`,
		`"auth_type":1`,
		`"auth_type_name":"oauth"`,
		`"type":"auth-file"`,
		`"provider":"anthropic"`,
		`"total_requests":10`,
		`"success_count":8`,
		`"failure_count":2`,
		`"input_tokens":100`,
		`"output_tokens":200`,
		`"reasoning_tokens":30`,
		`"cached_tokens":40`,
		`"total_tokens":370`,
		`"last_aggregated_usage_event_id":99`,
		`"first_used_at":"2026-05-04T08:00:00Z"`,
		`"last_used_at":"2026-05-04T09:00:00Z"`,
		`"stats_updated_at":"2026-05-04T10:00:00Z"`,
		`"is_deleted":true`,
		`"deleted_at":"2026-05-04T11:00:00Z"`,
	} {
		if !contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesRouteMasksAIProviderIdentity(t *testing.T) {
	rawLookupKey := "sk-live-secret-value"
	rawPrefix := "sk-live-prefix"
	maskedLookupKey := redact.APIKeyDisplayName(rawLookupKey)
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "", usageIdentitiesStub{items: []models.UsageIdentity{
		{ID: 1, Name: rawPrefix, AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: rawLookupKey, Type: "openai " + rawLookupKey, Provider: "OpenAI " + rawPrefix},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, rawLookupKey) || contains(body, rawPrefix) {
		t.Fatalf("expected raw AI provider lookup values to be hidden, got %s", body)
	}
	if !contains(body, `"identity":"`+maskedLookupKey+`"`) {
		t.Fatalf("expected masked AI provider identity %q in response body: %s", maskedLookupKey, body)
	}
}

func TestUsageIdentityReplacesLegacyMetadataRoutes(t *testing.T) {
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "", usageIdentitiesStub{})
	for _, path := range []string{"/api/v1/auth-files", "/api/v1/provider-metadata"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d: %s", path, resp.Code, resp.Body.String())
		}
	}
}
