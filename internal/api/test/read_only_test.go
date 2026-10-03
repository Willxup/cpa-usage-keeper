package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type readOnlyUsageStub struct {
	service.UsageProvider
	filter servicedto.UsageFilter
}

func (s *readOnlyUsageStub) GetUsageOverview(_ context.Context, f servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	s.filter = f
	return &servicedto.UsageOverviewSnapshot{Usage: &repodto.StatisticsSnapshot{TotalRequests: 7, TotalTokens: 700}}, nil
}
func (s *readOnlyUsageStub) GetUsageOverviewComparisons(_ context.Context, f servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	s.filter = f
	return &servicedto.UsageOverviewSnapshot{Comparisons: &repodto.UsageOverviewComparisonsRecord{
		APIKeys: map[string]*repodto.UsageComparisonItemRecord{
			"first-private-key":   {Key: "first-private-key", Label: "private-key-alias", Requests: 2, TotalTokens: 200},
			"second-private-key":  {Key: "second-private-key", Requests: 3, TotalTokens: 300, CostAvailable: true, CostUSD: 0},
			"revoked-private-key": {Key: "revoked-private-key", Requests: 2, TotalTokens: 200},
		}, AuthFiles: map[string]*repodto.UsageComparisonItemRecord{"private-file": {Key: "private-file"}},
	}}, nil
}

func TestReadOnlyLoginAndServerPermissions(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "admin-password-123456", ReadOnlyPassword: "read-only-password-123456"}
	usage := &readOnlyUsageStub{}
	keys := &authLoginKeyStub{rowsByID: map[int64]entities.CPAAPIKey{1: {ID: 1, APIKey: "first-private-key", KeyAlias: "private-key-alias"}, 2: {ID: 2, APIKey: "second-private-key"}, 3: {ID: 3, APIKey: "unused-private-key"}}}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, usage, nil, config, handler, "", OptionalProviders{CPAAPIKeys: keys, Quota: &quotaProviderStub{}, UsageIdentity: identityMetadataStub{}})
	for _, password := range []string{"", config.LoginPassword, "wrong-password"} {
		response := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/read-only-login", `{"password":"`+password+`"}`)
		if response.Code != 401 {
			t.Fatalf("incorrect password accepted: %d", response.Code)
		}
	}
	login := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/read-only-login", `{"password":"`+config.ReadOnlyPassword+`"}`)
	if login.Code != 204 {
		t.Fatal(login.Code)
	}
	cookie := login.Result().Cookies()[0]
	state := serveAPIGet(router, "/api/v1/auth/session", cookie)
	if !strings.Contains(state.Body.String(), `"role":"read_only"`) {
		t.Fatal(state.Body.String())
	}
	response := serveAPIGet(router, "/api/v1/read-only/overview?range=7d&api_key_id=999&auth_index=private&start=bad", cookie)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, private := range []string{"first-private-key", "second-private-key", "revoked-private-key", "unused-private-key", "private-key-alias", "private-file", "auth_index", "APIKey"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("leaked %s", private)
		}
	}
	var data struct {
		Keys []struct {
			Label    string
			Requests int
			Cost     *float64
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Keys) != 4 || data.Keys[0].Requests != 3 || data.Keys[0].Cost == nil || *data.Keys[0].Cost != 0 {
		t.Fatal(response.Body.String())
	}
	if usage.filter.APIKeyID != "" || usage.filter.AuthIndex != "" {
		t.Fatal("client supplied scope used")
	}
	if serveAPIGet(router, "/api/v1/read-only/overview?range=custom", cookie).Code != 400 {
		t.Fatal("unbounded period accepted")
	}
	if serveAPIGet(router, "/api/v1/read-only/key-quota", cookie).Code != 200 {
		t.Fatal("quota unavailable")
	}
	for _, path := range []string{"/api/v1/usage/overview", "/api/v1/usage/api-keys", "/api/v1/auth/sessions", "/api/v1/pricing", "/api/v1/status", "/api/v1/usage/identities"} {
		if serveAPIGet(router, path, cookie).Code != 403 {
			t.Fatalf("read-only session allowed admin read: %s", path)
		}
	}
	for _, item := range []struct{ method, path string }{
		{"PATCH", "/api/v1/auth-files/status"}, {"DELETE", "/api/v1/auth-files"}, {"PATCH", "/api/v1/auth-files/index/priority"},
		{"POST", "/api/v1/quota/reset"}, {"POST", "/api/v1/quota/refresh"}, {"PUT", "/api/v1/quota/auto-refresh/settings"},
		{"PUT", "/api/v1/pricing"}, {"PATCH", "/api/v1/usage/api-keys/1"}, {"POST", "/api/v1/usage/identities/1/stats/reset"}, {"DELETE", "/api/v1/auth/sessions/1"},
	} {
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(`{}`))
		req.AddCookie(cookie)
		req.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		if result.Code != 403 {
			t.Fatalf("write access %s %s: %d", item.method, item.path, result.Code)
		}
	}
	viewer, _, _ := sessions.CreateAPIKeyViewer(1)
	keyList := serveAPIGet(router, "/api/v1/read-only/keys", cookie)
	if keyList.Code != 200 || !strings.Contains(keyList.Body.String(), `"label":"Client key 1"`) || strings.Contains(keyList.Body.String(), "private") {
		t.Fatalf("unexpected safe key list: %d %s", keyList.Code, keyList.Body.String())
	}
	if serveAPIGet(router, "/api/v1/read-only/keys").Code != 401 || serveAPIGet(router, "/api/v1/read-only/keys", &http.Cookie{Name: "cpa_usage_keeper_session", Value: viewer}).Code != 403 {
		t.Fatal("key list role isolation failed")
	}

	if serveAPIGet(router, "/api/v1/read-only/overview", &http.Cookie{Name: "cpa_usage_keeper_session", Value: viewer}).Code != 403 {
		t.Fatal("client-key viewer got all-key access")
	}
	if serveAPIGet(router, "/api/v1/read-only/overview").Code != 401 {
		t.Fatal("anonymous all-key access")
	}
	admin, _, _ := sessions.Create()
	_ = NewAuthHandler(config, sessions)
	if !sessions.Validate(cookie.Value) || !sessions.Validate(admin) || !sessions.Validate(viewer) {
		t.Fatal("unchanged password revoked sessions")
	}
	config.ReadOnlyPassword = "changed-read-only-password"
	_ = NewAuthHandler(config, sessions)
	if sessions.Validate(cookie.Value) || !sessions.Validate(admin) || !sessions.Validate(viewer) {
		t.Fatal("password change failed to revoke only read-only sessions")
	}
}

func TestReadOnlyDisabledAndLoginLimits(t *testing.T) {
	for _, password := range []string{"", "short", "same-password-123456"} {
		sessions := auth.NewSessionManager(time.Hour)
		config := AuthConfig{Enabled: true, LoginPassword: "same-password-123456", ReadOnlyPassword: password}
		router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")
		response := serveCredentialMutation(router, "POST", "/api/v1/auth/read-only-login", `{"password":"`+password+`"}`)
		if response.Code != 401 {
			t.Fatal("disabled or unsafe password accepted")
		}
	}
	config := AuthConfig{Enabled: true, LoginPassword: "admin-password-123456", ReadOnlyPassword: "read-only-password-123456"}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, auth.NewSessionManager(time.Hour)), "")
	for i := 0; i < 5; i++ {
		if serveCredentialMutation(router, "POST", "/api/v1/auth/read-only-login", `{"password":"wrong"}`).Code != 401 {
			t.Fatal(i)
		}
	}
	if serveCredentialMutation(router, "POST", "/api/v1/auth/read-only-login", `{"password":"wrong"}`).Code != 429 {
		t.Fatal("login not rate limited")
	}
	if serveCredentialMutation(router, "POST", "/api/v1/auth/read-only-login", `{"password":"`+strings.Repeat("a", 5000)+`"}`).Code != 413 {
		t.Fatal("login body not bounded")
	}
}

func (s *readOnlyUsageStub) GetUsageActivity(_ context.Context, f servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error) {
	s.filter = f
	return &servicedto.UsageActivitySnapshot{}, nil
}
func (s *readOnlyUsageStub) GetUsageOverviewRealtime(_ context.Context, f servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	s.filter = f
	return &servicedto.UsageOverviewRealtime{CurrentUsage: servicedto.RealtimeCurrentUsage{
		APIKeys:   []servicedto.RealtimeUsageTopItem{{Key: "first-private-key", Label: "private-key-alias", Requests: 2}, {Key: "revoked-private-key", Requests: 3}},
		AuthFiles: []servicedto.RealtimeUsageTopItem{{Key: "private-file", Label: "private-email@example.invalid", Requests: 5}},
	}}, nil
}
func (s *readOnlyUsageStub) GetAnalysis(_ context.Context, f servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	s.filter = f
	return &servicedto.AnalysisSnapshot{
		APIKeyComposition:    []servicedto.AnalysisCompositionItem{{Key: "first-private-key", Requests: 2}, {Key: "revoked-private-key", Requests: 3}},
		AuthFilesComposition: []servicedto.AnalysisCompositionItem{{Key: "private-file", Label: "private-email@example.invalid", Requests: 5}},
		Heatmap:              []servicedto.AnalysisHeatmapCell{{APIKey: "first-private-key", Model: "public-model", Requests: 2}, {APIKey: "revoked-private-key", Model: "public-model", Requests: 3}},
	}, nil
}
func (s *readOnlyUsageStub) GetAnalysisLatency(_ context.Context, f servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error) {
	s.filter = f
	return &servicedto.AnalysisLatencyDiagnostics{}, nil
}

func TestReadOnlySharedDashboardReports(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "admin-password-123456", ReadOnlyPassword: "read-only-password-123456"}
	usage := &readOnlyUsageStub{}
	keys := &authLoginKeyStub{rowsByID: map[int64]entities.CPAAPIKey{1: {ID: 1, APIKey: "first-private-key", KeyAlias: "private-key-alias"}}}
	identities := identityMetadataStub{items: []entities.UsageIdentity{{ID: 1, Identity: "private-file", Type: "codex", Name: "private-email@example.invalid"}}}
	router := NewRouter(nil, nil, usage, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keys, UsageIdentity: identities})
	token, _, err := sessions.CreateReadOnlyWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "cpa_usage_keeper_session", Value: token}
	viewer, _, _ := sessions.CreateAPIKeyViewer(1)
	keyList := serveAPIGet(router, "/api/v1/read-only/keys", cookie)
	if keyList.Code != 200 || !strings.Contains(keyList.Body.String(), `"label":"Client key 1"`) || strings.Contains(keyList.Body.String(), "private") {
		t.Fatalf("unexpected safe key list: %d %s", keyList.Code, keyList.Body.String())
	}
	if serveAPIGet(router, "/api/v1/read-only/keys").Code != 401 || serveAPIGet(router, "/api/v1/read-only/keys", &http.Cookie{Name: "cpa_usage_keeper_session", Value: viewer}).Code != 403 {
		t.Fatal("key list role isolation failed")
	}

	for _, path := range []string{"key-overview", "key-overview/comparisons", "key-overview/realtime", "key-activity", "key-analysis", "key-analysis/latency"} {
		url := "/api/v1/read-only/" + path + "?range=today&api_key_id=1&auth_index=private-file&model=private-model"
		response := serveAPIGet(router, url, cookie)
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		for _, secret := range []string{"first-private-key", "revoked-private-key", "private-key-alias", "private-file", "private-email", "private-model"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("%s leaked %s", path, secret)
			}
		}
		if usage.filter.APIKeyID != "1" || usage.filter.AuthIndex != "" || usage.filter.Model != "" {
			t.Fatal("selected key not applied or unrelated selector accepted")
		}
		if serveAPIGet(router, "/api/v1/read-only/"+path+"?range=today", cookie).Code != 200 || usage.filter.APIKeyID != "" {
			t.Fatal("All keys did not clear selected scope")
		}
		if serveAPIGet(router, "/api/v1/read-only/"+path+"?range=today&api_key_id=bad", cookie).Code != 400 {
			t.Fatal("invalid key selector accepted")
		}
		if serveAPIGet(router, "/api/v1/read-only/"+path+"?range=today&api_key_id=999", cookie).Code != 404 {
			t.Fatal("unknown key selector accepted")
		}
		if path == "key-overview/comparisons" || path == "key-overview/realtime" || path == "key-analysis" {
			if !strings.Contains(response.Body.String(), "Codex account 1") || !strings.Contains(response.Body.String(), "Client key 1") || !strings.Contains(response.Body.String(), "Historical key") {
				t.Fatalf("missing account/key breakdown: %s", response.Body.String())
			}
		}
		if serveAPIGet(router, url).Code != 401 {
			t.Fatal("anonymous report allowed")
		}
		if serveAPIGet(router, url, &http.Cookie{Name: "cpa_usage_keeper_session", Value: viewer}).Code != 403 {
			t.Fatal("client-key report allowed")
		}
		req := httptest.NewRequest("POST", "/api/v1/read-only/"+path, strings.NewReader(`{}`))
		req.AddCookie(cookie)
		req.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		if result.Code != 404 {
			t.Fatalf("write on new report: %d", result.Code)
		}
	}
}
