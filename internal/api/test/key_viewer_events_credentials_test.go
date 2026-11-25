package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

type keyViewerQuotaStub struct {
	keeperapi.QuotaProvider
	refreshCalls []quota.RefreshRequest
}

func (s *keyViewerQuotaStub) GetCachedQuota(_ context.Context, req quota.CacheRequest) (quota.CacheResponse, error) {
	credits := 3
	fileName := "codex-alice@example.com.json"
	return quota.CacheResponse{
		Items: []quota.CachedQuotaItem{{
			AuthIndex: req.AuthIndexes[0],
			FileName:  &fileName,
			Status:    quota.RefreshTaskStatusCompleted,
			Quota: &quota.CheckResponse{
				ID:                                  req.AuthIndexes[0],
				Quota:                               []quota.QuotaRow{{Key: "primary", Label: "5h", RemainingFraction: floatPtr(0.75)}},
				RateLimitResetCreditsAvailableCount: &credits,
				ClaudeResetGrants:                   &quota.ClaudeResetGrantStatus{AvailableCount: 2},
			},
			UpstreamResponses: []quota.UpstreamResponse{{URL: "https://secret.example", Body: "raw-secret"}},
		}},
	}, nil
}

func (s *keyViewerQuotaStub) Refresh(_ context.Context, req quota.RefreshRequest) (quota.RefreshResponse, error) {
	s.refreshCalls = append(s.refreshCalls, req)
	return quota.RefreshResponse{
		Tasks:    []quota.RefreshTaskRef{{AuthIndex: req.AuthIndexes[0]}},
		Accepted: 1,
		Limit:    len(req.AuthIndexes),
	}, nil
}

func (s *keyViewerQuotaStub) GetRefreshTaskByAuthIndex(_ context.Context, authIndex string) (quota.RefreshTaskResponse, error) {
	credits := 2
	fileName := "codex-alice@example.com.json"
	return quota.RefreshTaskResponse{
		AuthIndex: authIndex,
		FileName:  &fileName,
		Status:    quota.RefreshTaskStatusCompleted,
		Quota: &quota.CheckResponse{
			ID:                                  authIndex,
			Quota:                               []quota.QuotaRow{{Key: "primary", Label: "5h", RemainingFraction: floatPtr(0.8)}},
			RateLimitResetCreditsAvailableCount: &credits,
			ClaudeResetGrants:                   &quota.ClaudeResetGrantStatus{AvailableCount: 1},
		},
		UpstreamResponses: []quota.UpstreamResponse{{URL: "https://secret.example", Body: "raw-secret"}},
	}, nil
}

func (s *keyViewerQuotaStub) GetCodexQuotaHistory(_ context.Context, _ quota.CodexQuotaHistoryRequest) (quota.CodexQuotaHistoryResponse, error) {
	return quota.CodexQuotaHistoryResponse{
		Windows: []quota.CodexQuotaHistoryWindow{},
		Cycles:  []quota.CodexQuotaHistoryCycle{},
	}, nil
}

func floatPtr(v float64) *float64 { return &v }

func TestAPIKeyViewerEventsAreStrictlyScopedToSessionKey(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-viewer123456", "sk-other654321"}, now); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	keys, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil || len(keys) != 2 {
		t.Fatalf("list API keys: %v %+v", err, keys)
	}
	var viewerKey, otherKey entities.CPAAPIKey
	for _, k := range keys {
		if k.APIKey == "sk-viewer123456" {
			viewerKey = k
		} else {
			otherKey = k
		}
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, viewerKey.ID, "Viewer Key"); err != nil {
		t.Fatalf("alias viewer key: %v", err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, otherKey.ID, "Secret Other Key"); err != nil {
		t.Fatalf("alias other key: %v", err)
	}
	cost := 0.12
	costOK := true
	for _, ev := range []entities.UsageEvent{
		{EventKey: "ev-viewer", APIGroupKey: viewerKey.APIKey, Model: "gpt-5-viewer", AuthType: "oauth", Source: "codex-a@example.com", AuthIndex: "auth-a", Timestamp: now.Add(-2 * time.Minute), TotalTokens: 100, CostUSD: &cost, CostAvailable: &costOK},
		{EventKey: "ev-other", APIGroupKey: otherKey.APIKey, Model: "claude-secret-other", AuthType: "oauth", Source: "codex-b@example.com", AuthIndex: "auth-b", Timestamp: now.Add(-1 * time.Minute), TotalTokens: 999, CostUSD: &cost, CostAvailable: &costOK},
	} {
		if err := db.Create(&ev).Error; err != nil {
			t.Fatalf("seed event %s: %v", ev.EventKey, err)
		}
	}

	sessions := auth.NewSessionManager(time.Hour)
	viewerToken, _, err := sessions.CreateAPIKeyViewerWithSource(viewerKey.ID, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(
		nil,
		nil,
		service.NewUsageService(db, pricing.NewCatalog(nil)),
		nil,
		config,
		keeperapi.NewAuthHandler(config, sessions),
		"",
		keeperapi.OptionalProviders{
			CPAAPIKeys:    service.NewCPAAPIKeyService(db),
			UsageIdentity: service.NewUsageIdentityService(db),
		},
	)
	viewerCookie := &http.Cookie{Name: standardSessionCookieName, Value: viewerToken}

	// Attempt to spoof api_key_id to otherKey.ID: must still only return viewerKey's event, with masked source account name.
	eventsResp := serveAPIGet(router, "/api/v1/key-events?range=24h&api_key_id="+strings.TrimSpace(jsonNumber(otherKey.ID)), viewerCookie)
	if eventsResp.Code != http.StatusOK {
		t.Fatalf("key-events status=%d body=%s", eventsResp.Code, eventsResp.Body.String())
	}
	body := eventsResp.Body.String()
	if !strings.Contains(body, "gpt-5-viewer") || strings.Contains(body, "claude-secret-other") || strings.Contains(body, "Secret Other Key") || strings.Contains(body, "sk-other654321") || strings.Contains(body, "codex-a@example.com") {
		t.Fatalf("key-events leaked other key or raw account name: %s", body)
	}
	if !strings.Contains(body, "co***-a@e***.com") {
		t.Fatalf("expected masked source account name in key-events: %s", body)
	}

	// Model filter options must only return models used by the viewer's key.
	modelsResp := serveAPIGet(router, "/api/v1/key-events/filters/models", viewerCookie)
	if modelsResp.Code != http.StatusOK {
		t.Fatalf("key-events model filters status=%d body=%s", modelsResp.Code, modelsResp.Body.String())
	}
	if !strings.Contains(modelsResp.Body.String(), "gpt-5-viewer") || strings.Contains(modelsResp.Body.String(), "claude-secret-other") {
		t.Fatalf("model filter options leaked other key models: %s", modelsResp.Body.String())
	}

	// Export must also strictly stay scoped to the viewer's key.
	exportResp := serveAPIGet(router, "/api/v1/key-events/export?range=24h&format=json&api_key_id="+jsonNumber(otherKey.ID), viewerCookie)
	if exportResp.Code != http.StatusOK {
		t.Fatalf("key-events export status=%d body=%s", exportResp.Code, exportResp.Body.String())
	}
	if !strings.Contains(exportResp.Body.String(), "gpt-5-viewer") || strings.Contains(exportResp.Body.String(), "claude-secret-other") {
		t.Fatalf("key-events export leaked other key data: %s", exportResp.Body.String())
	}
}

func TestAPIKeyViewerCredentialsAndLiveQuotaAreReadOnlyAndSanitized(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-viewer123456"}, now); err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	keys, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil || len(keys) != 1 {
		t.Fatalf("list API keys: %v", err)
	}
	credFile := "codex-alice@example.com.json"
	credPath := "/secret/path/codex-alice@example.com.json"
	credNote := "private-note-123"
	cred := entities.UsageIdentity{
		Name:         "alice@example.com",
		AuthType:     entities.UsageIdentityAuthTypeCredential,
		AuthTypeName: "oauth",
		Identity:     "auth-codex-1",
		Type:         "codex",
		Provider:     "codex",
		FileName:     &credFile,
		FilePath:     &credPath,
		Note:         &credNote,
	}
	aiProvider := entities.UsageIdentity{
		Name:         "openai-provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "provider-openai-1",
		Type:         "openai",
		Provider:     "openai",
	}
	if err := db.Create(&cred).Error; err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := db.Create(&aiProvider).Error; err != nil {
		t.Fatalf("seed ai provider: %v", err)
	}

	sessions := auth.NewSessionManager(time.Hour)
	viewerToken, _, err := sessions.CreateAPIKeyViewerWithSource(keys[0].ID, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	quotaStub := &keyViewerQuotaStub{}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(
		nil,
		nil,
		service.NewUsageService(db, pricing.NewCatalog(nil)),
		nil,
		config,
		keeperapi.NewAuthHandler(config, sessions),
		"",
		keeperapi.OptionalProviders{
			CPAAPIKeys:    service.NewCPAAPIKeyService(db),
			UsageIdentity: service.NewUsageIdentityService(db),
			Quota:         quotaStub,
		},
	)
	viewerCookie := &http.Cookie{Name: standardSessionCookieName, Value: viewerToken}

	// 1. Credentials page returns OAuth credentials only with masked names and no file_name/file_path/note.
	pageResp := serveAPIGet(router, "/api/v1/key-credentials/page?auth_type=2&page=1&page_size=10", viewerCookie)
	if pageResp.Code != http.StatusOK {
		t.Fatalf("key-credentials page status=%d body=%s", pageResp.Code, pageResp.Body.String())
	}
	pageBody := pageResp.Body.String()
	if !strings.Contains(pageBody, "al***ce@e***.com") || strings.Contains(pageBody, "alice@example.com") || strings.Contains(pageBody, "codex-alice") || strings.Contains(pageBody, "private-note-123") || strings.Contains(pageBody, "provider-openai-1") {
		t.Fatalf("key-credentials page returned unexpected or unmasked identities: %s", pageBody)
	}

	// 2. Credential detail works for OAuth credential (masked, no file_name/file_path/note), returns 404 for AI provider.
	detailResp := serveAPIGet(router, "/api/v1/key-credentials/"+jsonNumber(cred.ID), viewerCookie)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("key-credential detail status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	detailBody := detailResp.Body.String()
	if !strings.Contains(detailBody, "al***ce@e***.com") || strings.Contains(detailBody, "alice@example.com") || strings.Contains(detailBody, "codex-alice") || strings.Contains(detailBody, "private-note-123") {
		t.Fatalf("key-credential detail leaked raw account info: %s", detailBody)
	}
	providerDetailResp := serveAPIGet(router, "/api/v1/key-credentials/"+jsonNumber(aiProvider.ID), viewerCookie)
	if providerDetailResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for AI provider detail on key-credentials, got %d", providerDetailResp.Code)
	}

	// 3. Cached quota works and strips file_name, reset credits, Claude reset grants, and upstream responses.
	cacheReq := httptest.NewRequest(http.MethodPost, "/api/v1/key-quota/cache", strings.NewReader(`{"auth_indexes":["auth-codex-1"]}`))
	cacheReq.Header.Set("Content-Type", "application/json")
	cacheReq.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
	cacheReq.AddCookie(viewerCookie)
	cacheResp := httptest.NewRecorder()
	router.ServeHTTP(cacheResp, cacheReq)
	if cacheResp.Code != http.StatusOK {
		t.Fatalf("key-quota cache status=%d body=%s", cacheResp.Code, cacheResp.Body.String())
	}
	cacheBody := cacheResp.Body.String()
	if strings.Contains(cacheBody, "alice@example.com") || strings.Contains(cacheBody, "file_name") || strings.Contains(cacheBody, "rateLimitResetCreditsAvailableCount") || strings.Contains(cacheBody, "claudeResetGrants") || strings.Contains(cacheBody, "raw-secret") {
		t.Fatalf("key-quota cache leaked file_name/reset/upstream fields: %s", cacheBody)
	}

	// 4. Live quota refresh is allowed for key viewers!
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/key-quota/refresh", strings.NewReader(`{"auth_indexes":["auth-codex-1"]}`))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshReq.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
	refreshReq.AddCookie(viewerCookie)
	refreshResp := httptest.NewRecorder()
	router.ServeHTTP(refreshResp, refreshReq)
	if refreshResp.Code != http.StatusOK || len(quotaStub.refreshCalls) != 1 {
		t.Fatalf("key-quota refresh status=%d calls=%d body=%s", refreshResp.Code, len(quotaStub.refreshCalls), refreshResp.Body.String())
	}

	// 5. Polling refresh task works and strips file_name, reset credits, grants, and upstream fields.
	taskResp := serveAPIGet(router, "/api/v1/key-quota/refresh/auth-codex-1", viewerCookie)
	if taskResp.Code != http.StatusOK {
		t.Fatalf("key-quota refresh task status=%d body=%s", taskResp.Code, taskResp.Body.String())
	}
	if strings.Contains(taskResp.Body.String(), "alice@example.com") || strings.Contains(taskResp.Body.String(), "file_name") || strings.Contains(taskResp.Body.String(), "rateLimitResetCreditsAvailableCount") || strings.Contains(taskResp.Body.String(), "claudeResetGrants") || strings.Contains(taskResp.Body.String(), "raw-secret") {
		t.Fatalf("key-quota refresh task leaked file_name/reset/upstream fields: %s", taskResp.Body.String())
	}

	// 6. Quota history read works.
	historyResp := serveAPIGet(router, "/api/v1/key-quota/history/auth-codex-1", viewerCookie)
	if historyResp.Code != http.StatusOK {
		t.Fatalf("key-quota history status=%d body=%s", historyResp.Code, historyResp.Body.String())
	}

	// 7. All admin write & sensitive routes stay strictly forbidden (403) for api_key_viewer!
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/usage/api-keys/settings", ""},
		{http.MethodGet, "/api/v1/auth/sessions", ""},
		{http.MethodPatch, "/api/v1/credentials/auth-codex-1/status", `{"disabled":true}`},
		{http.MethodPatch, "/api/v1/credentials/auth-codex-1/priority", `{"priority":5}`},
		{http.MethodDelete, "/api/v1/credentials", `{"names":["codex-alice@example.com.json"]}`},
		{http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"auth-codex-1"}`},
		{http.MethodDelete, "/api/v1/quota/history/auth-codex-1/cycles/1", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
		}
		req.AddCookie(viewerCookie)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer %s %s got %d, want 403: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
