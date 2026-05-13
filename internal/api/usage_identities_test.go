package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
)

type usageIdentitiesStub struct {
	items            []entities.UsageIdentity
	activeItems      []entities.UsageIdentity
	pagedActiveItems []entities.UsageIdentity
	pagedActiveTotal int64
	pagedActiveReq   *service.ListUsageIdentitiesRequest
	err              error
}

func (s usageIdentitiesStub) ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	if s.activeItems != nil {
		return s.activeItems, s.err
	}
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentitiesPage(_ context.Context, request service.ListUsageIdentitiesRequest) (service.ListUsageIdentitiesResponse, error) {
	if s.pagedActiveReq != nil {
		*s.pagedActiveReq = request
	}
	if s.pagedActiveItems != nil || s.pagedActiveTotal != 0 {
		return service.ListUsageIdentitiesResponse{Items: s.pagedActiveItems, Total: s.pagedActiveTotal}, s.err
	}
	return service.ListUsageIdentitiesResponse{Items: s.items, Total: int64(len(s.items))}, s.err
}

func TestUsageIdentitiesRouteReturnsMetadataStatsAndActiveRows(t *testing.T) {
	firstUsedAt := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	lastUsedAt := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	statsUpdatedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	deletedAt := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)

	activeIdentity := entities.UsageIdentity{
		ID:                         1,
		Name:                       "Claude Desktop",
		AuthType:                   entities.UsageIdentityAuthTypeAuthFile,
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
	}
	deletedIdentity := entities.UsageIdentity{
		ID:           2,
		Name:         "Deleted Provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "sk-deleted-provider-secret",
		Type:         "openai",
		Provider:     "OpenAI",
		IsDeleted:    true,
		DeletedAt:    &deletedAt,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{
		items:       []entities.UsageIdentity{activeIdentity, deletedIdentity},
		activeItems: []entities.UsageIdentity{activeIdentity},
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
	if contains(body, "Deleted Provider") || contains(body, "sk-deleted-provider-secret") || contains(body, `"deleted_at"`) {
		t.Fatalf("expected deleted identities to be filtered from response, got %s", body)
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
		`"is_deleted":false`,
	} {
		if !contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesRouteDoesNotReturnUnpublishedMetadataFields(t *testing.T) {
	activeStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	activeUntil := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	accountID := "acct_123"
	planType := "team"
	baseURL := "https://api.openai.com/v1"
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:           1,
		Name:         "Codex Account",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "codex-auth",
		Type:         "codex",
		Provider:     "Codex",
		Prefix:       "codex-prefix",
		BaseURL:      baseURL,
		AccountID:    &accountID,
		ActiveStart:  &activeStart,
		ActiveUntil:  &activeUntil,
		PlanType:     &planType,
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	for _, expected := range []string{
		`"plan_type":"team"`,
		`"active_start":"2026-05-01T00:00:00Z"`,
		`"active_until":"2026-06-01T00:00:00Z"`,
	} {
		if !contains(body, expected) {
			t.Fatalf("expected API response to include %s, got %s", expected, body)
		}
	}
	for _, forbidden := range []string{
		`"prefix"`,
		`"base_url"`,
		`"account_id"`,
	} {
		if contains(body, forbidden) {
			t.Fatalf("expected API response not to include %s, got %s", forbidden, body)
		}
	}
}

func TestUsageIdentitiesPageRouteFiltersByAuthTypeAndPaginates(t *testing.T) {
	captured := service.ListUsageIdentitiesRequest{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{
		pagedActiveReq:   &captured,
		pagedActiveTotal: 25,
		pagedActiveItems: []entities.UsageIdentity{{
			ID:           11,
			Name:         "Codex Account",
			AuthType:     entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "oauth",
			Identity:     "codex-auth",
			Type:         "codex",
			Provider:     "Codex",
		}},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/page?auth_type=1&page=2&page_size=10", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if captured.AuthType == nil || *captured.AuthType != entities.UsageIdentityAuthTypeAuthFile || captured.Page != 2 || captured.PageSize != 10 {
		t.Fatalf("expected auth_type/page/page_size request, got %+v", captured)
	}
	for _, expected := range []string{`"identities":[`, `"id":11`, `"total_count":25`, `"page":2`, `"page_size":10`, `"total_pages":3`} {
		if !contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesRouteReturnsProviderDisplayName(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:           1,
		Name:         "Provider Name",
		Prefix:       "Team Prefix",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "provider-auth-index",
		Type:         "openai",
		Provider:     "OpenAI",
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"displayName":"Provider Name(Team Prefix)"`) {
		t.Fatalf("expected displayName with name and prefix, got %s", body)
	}
	if contains(body, `"prefix"`) {
		t.Fatalf("expected raw prefix field to stay unpublished, got %s", body)
	}
}

func TestUsageIdentitiesRouteMasksAIProviderIdentity(t *testing.T) {
	rawLookupKey := "sk-live-secret-value"
	maskedLookupKey := redact.APIKeyDisplayName(rawLookupKey)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{
		{ID: 1, Name: "Provider Name", Prefix: "Team Prefix", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: rawLookupKey, Type: "openai", Provider: "OpenAI"},
	}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, rawLookupKey) {
		t.Fatalf("expected raw AI provider lookup key to be hidden, got %s", body)
	}
	if !contains(body, `"identity":"`+maskedLookupKey+`"`) {
		t.Fatalf("expected masked AI provider identity %q in response body: %s", maskedLookupKey, body)
	}
	if !contains(body, `"name":"Provider Name"`) || !contains(body, `"provider":"OpenAI"`) || !contains(body, `"displayName":"Provider Name(Team Prefix)"`) {
		t.Fatalf("expected AI provider display fields to use usage_identities values directly, got %s", body)
	}
}

func TestUsageIdentityReplacesLegacyMetadataRoutes(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{}})
	for _, path := range []string{"/api/v1/auth-files", "/api/v1/provider-metadata"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d: %s", path, resp.Code, resp.Body.String())
		}
	}
}
