package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
)

type authCPAAPIKeyStub struct {
	row         entities.CPAAPIKey
	findErr     error
	byValueKey  string
	byIDCalls   int
	byValueCall int
}

func (s *authCPAAPIKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByValue(_ context.Context, apiKey string) (entities.CPAAPIKey, error) {
	s.byValueCall++
	s.byValueKey = apiKey
	if s.findErr != nil {
		return entities.CPAAPIKey{}, s.findErr
	}
	return s.row, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	s.byIDCalls++
	if s.findErr != nil {
		return entities.CPAAPIKey{}, s.findErr
	}
	return s.row, nil
}

func (s *authCPAAPIKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func TestAuthSessionReportsAuthenticatedWhenDisabled(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{Enabled: false}, nil, "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthProtectedRouteRequiresSessionWhenEnabled(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestAuthLoginSetsCookieAndUnlocksProtectedRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}
	cookie := loginResp.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
	if cookie[0].Name != sessionCookieName {
		t.Fatalf("expected cookie %q, got %q", sessionCookieName, cookie[0].Name)
	}
	if cookie[0].Path != "/" {
		t.Fatalf("expected root cookie path '/', got %q", cookie[0].Path)
	}

	usageResp := httptest.NewRecorder()
	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)
	usageReq.AddCookie(cookie[0])
	router.ServeHTTP(usageResp, usageReq)

	if usageResp.Code != http.StatusOK {
		t.Fatalf("expected protected route to succeed, got %d %s", usageResp.Code, usageResp.Body.String())
	}
}

func TestAuthSessionReturnsAdminRoleAfterPasswordLogin(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}

	sessionResp := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionReq.AddCookie(loginResp.Result().Cookies()[0])
	router.ServeHTTP(sessionResp, sessionReq)

	if sessionResp.Code != http.StatusOK || !contains(sessionResp.Body.String(), `"authenticated":true`) || !contains(sessionResp.Body.String(), `"role":"admin"`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, sessionResp.Body.String())
	}
}

func TestAuthAPIKeyLoginSetsViewerSessionCookieAndSessionSummary(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-live123456", DisplayKey: "sk-l************3456", KeyAlias: "Team Key"}}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/cpa/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"sk-live123456"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected API key login status 204, got %d %s", loginResp.Code, loginResp.Body.String())
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Path != "/cpa" {
		t.Fatalf("expected auth cookie with /cpa path, got %+v", cookies)
	}
	if keyProvider.byValueKey != "sk-live123456" {
		t.Fatalf("expected login to pass API key to provider, got %q", keyProvider.byValueKey)
	}

	sessionResp := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/cpa/api/v1/auth/session", nil)
	sessionReq.AddCookie(cookies[0])
	router.ServeHTTP(sessionResp, sessionReq)

	body := sessionResp.Body.String()
	if sessionResp.Code != http.StatusOK || !contains(body, `"authenticated":true`) || !contains(body, `"role":"api_key_viewer"`) || !contains(body, `"api_key":{"display_key":"sk-*********123456","alias":"Team Key"}`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, body)
	}
	if contains(body, "sk-live123456") || contains(body, "sk-l************3456") {
		t.Fatalf("expected session response not to expose raw API key: %s", body)
	}
}

func TestAuthAPIKeyLoginFailuresAreGenericUnauthorized(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, body := range []string{`{"apiKey":"missing"}`, `{bad json}`} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized || !contains(resp.Body.String(), "invalid credentials") {
			t.Fatalf("expected generic 401 for %s, got %d %s", body, resp.Code, resp.Body.String())
		}
	}
}

func TestAuthAPIKeyLoginRateLimitsRepeatedFailures(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for i := 0; i < maxFailedLoginAttempts; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"missing"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.10:1234"
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized || !contains(resp.Body.String(), "invalid credentials") {
			t.Fatalf("expected failed attempt %d to return generic 401, got %d %s", i+1, resp.Code, resp.Body.String())
		}
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"missing"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.10:1234"
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeated failed API key attempts to return 429, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthAPIKeyLoginSuccessClearsFailedAttempts(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for i := 0; i < maxFailedLoginAttempts; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"missing"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.11:1234"
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed attempt %d to return 401, got %d", i+1, resp.Code)
		}
	}

	keyProvider.findErr = nil
	keyProvider.row = entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}
	successResp := httptest.NewRecorder()
	successReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"sk-live"}`))
	successReq.Header.Set("Content-Type", "application/json")
	successReq.RemoteAddr = "198.51.100.11:1234"
	router.ServeHTTP(successResp, successReq)
	if successResp.Code != http.StatusNoContent {
		t.Fatalf("expected successful API key login to be allowed and clear failed attempts, got %d %s", successResp.Code, successResp.Body.String())
	}

	keyProvider.findErr = context.Canceled
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/api-key-login", strings.NewReader(`{"apiKey":"missing"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.11:1234"
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected first failed attempt after successful API key login to return 401, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthSessionClearsInactiveViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cpa/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected inactive session response: %d %s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive viewer session to be deleted")
	}
	cookies := resp.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != sessionCookieName || cookies[0].Path != "/cpa" || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected session cookie to be cleared, got %+v", cookies)
	}
}

func TestAuthLogoutClearsKeyOverviewRateLimitForSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	if !handler.allowKeyOverviewRequest(token) {
		t.Fatal("expected initial key overview request to be allowed")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected logout status 204, got %d", resp.Code)
	}
	if _, ok := handler.keyOverviewRequests[token]; ok {
		t.Fatal("expected logout to clear key overview rate limit entry")
	}
}

func TestAuthSessionClearsKeyOverviewRateLimitForInactiveViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "", OptionalProviders{CPAAPIKeys: keyProvider})

	if !handler.allowKeyOverviewRequest(token) {
		t.Fatal("expected initial key overview request to be allowed")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected inactive session response: %d %s", resp.Code, resp.Body.String())
	}
	if _, ok := handler.keyOverviewRequests[token]; ok {
		t.Fatal("expected inactive viewer session cleanup to clear key overview rate limit entry")
	}
}

func TestAuthSessionClearsKeyOverviewRateLimitForExpiredSession(t *testing.T) {
	sessions := auth.NewSessionManager(-time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: -time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	if !handler.allowKeyOverviewRequest(token) {
		t.Fatal("expected initial key overview request to be allowed")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected expired session response: %d %s", resp.Code, resp.Body.String())
	}
	if _, ok := handler.keyOverviewRequests[token]; ok {
		t.Fatal("expected expired auth session cleanup to clear key overview rate limit entry")
	}
}

func TestAuthMiddlewareClearsKeyOverviewRateLimitForExpiredSession(t *testing.T) {
	sessions := auth.NewSessionManager(-time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: -time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	if !handler.allowKeyOverviewRequest(token) {
		t.Fatal("expected initial key overview request to be allowed")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired session to return 401, got %d %s", resp.Code, resp.Body.String())
	}
	if _, ok := handler.keyOverviewRequests[token]; ok {
		t.Fatal("expected expired middleware session cleanup to clear key overview rate limit entry")
	}
}

func TestViewerSessionCannotAccessAdminManagementRoutes(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, path := range []string{"/api/v1/usage/api-keys", "/api/v1/usage/api-keys/settings"} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusForbidden {
			t.Fatalf("%s: expected viewer session to be forbidden from admin route, got %d %s", path, resp.Code, resp.Body.String())
		}
	}
}

func TestViewerSessionCannotAccessStatusActiveRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	recorder := &activeStatusRecorderStub{}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{
		Status: StatusRouteConfig{ActiveRecorder: recorder},
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status/active", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected viewer session to be forbidden from status active route, got %d %s", resp.Code, resp.Body.String())
	}
	if recorder.calls != 0 {
		t.Fatalf("expected forbidden viewer heartbeat not to record activity, got %d calls", recorder.calls)
	}
}

func TestAdminSessionCannotAccessKeyOverviewRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, &usageFilterStub{}, nil, config, NewAuthHandler(config, sessions), "")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=24h", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected admin session to be forbidden from key overview route, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthLoginRejectsWrongPassword(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestAuthLoginRateLimitsRepeatedFailures(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	for i := 0; i < 5; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.1:1234"
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed attempt %d to return 401, got %d", i+1, resp.Code)
		}
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.1:1234"
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeated failed attempts to return 429, got %d", resp.Code)
	}
}

func TestAuthLoginAllowsCorrectPasswordAfterRateLimitThreshold(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	for i := 0; i < 5; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.2:1234"
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed attempt %d to return 401, got %d", i+1, resp.Code)
		}
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.2:1234"
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected correct password to clear failed attempts and login, got %d", resp.Code)
	}
}

func TestAuthLogoutDeletesSessionCookie(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected auth cookie to be set")
	}

	logoutResp := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookies[0])
	router.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("expected logout status 204, got %d", logoutResp.Code)
	}
	clearCookies := logoutResp.Result().Cookies()
	if len(clearCookies) == 0 || clearCookies[0].Name != sessionCookieName || clearCookies[0].MaxAge >= 0 {
		t.Fatalf("expected logout to clear session cookie, got %+v", clearCookies)
	}

	usageResp := httptest.NewRecorder()
	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)
	usageReq.AddCookie(cookies[0])
	router.ServeHTTP(usageResp, usageReq)
	if usageResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected logged out session to be rejected, got %d", usageResp.Code)
	}
}

func TestSubpathAuthUsesPrefixedRoutesAndCookiePath(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "/cpa")

	sessionResp := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/cpa/api/v1/auth/session", nil)
	router.ServeHTTP(sessionResp, sessionReq)
	if sessionResp.Code != http.StatusOK || !contains(sessionResp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, sessionResp.Body.String())
	}

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/cpa/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
	if cookies[0].Path != "/cpa" {
		t.Fatalf("expected subpath cookie path '/cpa', got %q", cookies[0].Path)
	}

	usageResp := httptest.NewRecorder()
	usageReq := httptest.NewRequest(http.MethodGet, "/cpa/api/v1/usage/overview", nil)
	usageReq.AddCookie(cookies[0])
	router.ServeHTTP(usageResp, usageReq)
	if usageResp.Code != http.StatusOK {
		t.Fatalf("expected protected route under subpath to succeed, got %d %s", usageResp.Code, usageResp.Body.String())
	}

	unprefixedResp := httptest.NewRecorder()
	unprefixedReq := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)
	unprefixedReq.AddCookie(cookies[0])
	router.ServeHTTP(unprefixedResp, unprefixedReq)
	if unprefixedResp.Code != http.StatusNotFound {
		t.Fatalf("expected unprefixed route to 404, got %d", unprefixedResp.Code)
	}
}
