package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestAPIProviderViewerQuotaUsesOnlyActiveSanitizedCache(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, _ := sessions.CreateAPIKeyViewer(42)
	at := time.Now()
	used := 25.0
	provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{Items: []quota.CachedQuotaItem{{AuthIndex: "api-fixture", Status: quota.RefreshTaskStatusCompleted, RefreshedAt: &at, Quota: &quota.CheckResponse{ID: "private-id", Quota: []quota.QuotaRow{{Label: "Weekly", UsedPercent: &used, CapturedAt: at, Source: "api_response_headers"}}}}}}}
	identities := identityMetadataStub{items: []entities.UsageIdentity{
		{ID: 1, Identity: "api-fixture", AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "codex", Name: "private name", BaseURL: "https://private.example.invalid"},
		{ID: 2, Identity: "disabled-api", AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "codex", Disabled: new(true)},
		{ID: 3, Identity: "deleted-api", AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "claude", IsDeleted: true},
	}}
	config := AuthConfig{Enabled: true, APIKeyViewerQuotaEnabled: true}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{Quota: provider, UsageIdentity: identities, CPAAPIKeys: &keyViewerRankingKeyStub{row: entities.CPAAPIKey{ID: 42}}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, viewerRankingRequest(http.MethodGet, "/api/v1/key-quota?auth_indexes=disabled-api", token))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, expected := range []string{`"kind":"api"`, `"status":"available"`, `"remaining_percent":75`, `"source":"api_response_headers"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatal(response.Body.String())
		}
	}
	for _, secret := range []string{"api-fixture", "disabled-api", "deleted-api", "private name", "private.example.invalid", "private-id"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("API quota leaked %q", secret)
		}
	}
	if len(provider.cacheRequest.AuthIndexes) != 1 || provider.cacheRequest.AuthIndexes[0] != "api-fixture" {
		t.Fatal(provider.cacheRequest)
	}
}
