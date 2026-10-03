package test

import (
	"encoding/json"
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

func TestKeyQuotaPermissionsAndSanitization(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			sessions := auth.NewSessionManager(time.Hour)
			token, _, _ := sessions.CreateAPIKeyViewer(42)
			old := time.Now().Add(-time.Hour)
			filename := "private-email@example.com.json"
			used := 32.5
			provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{Items: []quota.CachedQuotaItem{{
				AuthIndex: "secret-index", FileName: &filename, Status: quota.RefreshTaskStatusCompleted, RefreshedAt: &old, Error: "secret-error",
				Quota: &quota.CheckResponse{ID: "private-id", Subscription: &quota.SubscriptionInfo{Plan: "private-plan"}, Quota: []quota.QuotaRow{{Label: "Weekly", UsedPercent: &used, ResetAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}},
			}}}}
			identities := identityMetadataStub{items: []entities.UsageIdentity{
				{ID: 1, Identity: "secret-index", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, Name: filename, FileName: &filename},
				{ID: 2, Identity: "disabled-index", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: new(true)},
				{ID: 3, Identity: "api-secret", Type: "openai-compatibility", AuthType: entities.UsageIdentityAuthTypeAIProvider},
			}}
			config := AuthConfig{Enabled: true, LoginPassword: "test-password", SessionTTL: time.Hour, APIKeyViewerQuotaEnabled: enabled}
			keys := &keyViewerRankingKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "test-client"}}
			router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{Quota: provider, UsageIdentity: identities, CPAAPIKeys: keys})
			read := func(path, token string) *httptest.ResponseRecorder {
				r := httptest.NewRecorder()
				router.ServeHTTP(r, viewerRankingRequest(http.MethodGet, path, token))
				return r
			}
			response := read("/api/v1/key-quota?auth_indexes=disabled-index", token)
			if !enabled {
				if response.Code != 404 {
					t.Fatal(response.Code)
				}
				return
			}
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			body := response.Body.String()
			for _, secret := range []string{"secret-index", filename, "private-id", "private-plan", "secret-error", "disabled-index", "api-secret"} {
				if strings.Contains(body, secret) {
					t.Fatalf("leaked %q", secret)
				}
			}
			if len(provider.cacheRequest.AuthIndexes) != 1 || provider.cacheRequest.AuthIndexes[0] != "secret-index" {
				t.Fatalf("untrusted account selection: %+v", provider.cacheRequest)
			}
			var data struct {
				Accounts []struct {
					Status string
					Rows   []struct {
						RemainingPercent *float64 `json:"remaining_percent"`
					}
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data.Accounts) != 2 || data.Accounts[0].Status != "stale" || data.Accounts[1].Status != "unavailable" || *data.Accounts[0].Rows[0].RemainingPercent != 67.5 {
				t.Fatal(body)
			}
			if read("/api/v1/key-quota", "").Code != 401 {
				t.Fatal("anonymous quota allowed")
			}
			session := read("/api/v1/auth/session", token)
			if !strings.Contains(session.Body.String(), `"quota_enabled":true`) {
				t.Fatal("missing session capability")
			}
			for _, path := range []string{"/api/v1/quota/cache", "/api/v1/quota/refresh", "/api/v1/quota/reset"} {
				req := viewerRankingRequest(http.MethodPost, path, token)
				req.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				if resp.Code != 403 {
					t.Fatalf("viewer reached %s: %d", path, resp.Code)
				}
			}
			keys.row.ID = 99
			if read("/api/v1/key-quota", token).Code != 401 {
				t.Fatal("revoked viewer retained quota access")
			}
		})
	}
}

func TestKeyQuotaEmptyProviders(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, _ := sessions.CreateAPIKeyViewer(42)
	config := AuthConfig{Enabled: true, APIKeyViewerQuotaEnabled: true}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{Quota: &quotaProviderStub{}, UsageIdentity: identityMetadataStub{}, CPAAPIKeys: &keyViewerRankingKeyStub{row: entities.CPAAPIKey{ID: 42}}})
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, viewerRankingRequest(http.MethodGet, "/api/v1/key-quota", token))
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"accounts":[]`) {
		t.Fatal(resp.Code, resp.Body.String())
	}
}
