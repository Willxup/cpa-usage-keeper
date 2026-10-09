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
	"cpa-usage-keeper/internal/service"
)

type authLoginKeyStub struct {
	service.CPAAPIKeyProvider
	row        entities.CPAAPIKey
	rowsByID   map[int64]entities.CPAAPIKey
	findErr    error
	byValueKey string
}

func (s *authLoginKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	if len(s.rowsByID) > 0 {
		rows := make([]entities.CPAAPIKey, 0, len(s.rowsByID))
		for _, row := range s.rowsByID {
			rows = append(rows, row)
		}
		return rows, nil
	}
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *authLoginKeyStub) FindActiveCPAAPIKeyByValue(_ context.Context, apiKey string) (entities.CPAAPIKey, error) {
	s.byValueKey = apiKey
	if s.findErr != nil {
		return entities.CPAAPIKey{}, s.findErr
	}
	return s.row, nil
}

func (s *authLoginKeyStub) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if s.findErr != nil {
		return entities.CPAAPIKey{}, s.findErr
	}
	if len(s.rowsByID) > 0 {
		row, ok := s.rowsByID[id]
		if ok {
			return row, nil
		}
		return entities.CPAAPIKey{}, context.Canceled
	}
	return s.row, nil
}

func TestAuthSessionReportsAuthenticatedWhenDisabled(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{Enabled: false}, nil, "")
	resp := serveAPIGet(router, "/api/v1/auth/session")

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthProtectedRouteRequiresSessionWhenEnabled(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")
	resp := serveAPIGet(router, "/api/v1/usage/overview")

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestAuthLoginSetsCookieAndUnlocksProtectedRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	loginResp := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/login", `{"password":"secret"}`)

	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}
	cookie := loginResp.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected auth cookie to be set")
	}
	if cookie[0].Name != standardSessionCookieName {
		t.Fatalf("expected cookie %q, got %q", standardSessionCookieName, cookie[0].Name)
	}
	if cookie[0].Path != "/" {
		t.Fatalf("expected root cookie path '/', got %q", cookie[0].Path)
	}

	usageResp := serveAPIGet(router, "/api/v1/usage/overview", cookie[0])

	if usageResp.Code != http.StatusOK {
		t.Fatalf("expected protected route to succeed, got %d %s", usageResp.Code, usageResp.Body.String())
	}
	sessionResp := serveAPIGet(router, "/api/v1/auth/session", cookie[0])
	if sessionResp.Code != http.StatusOK || !strings.Contains(sessionResp.Body.String(), `"authenticated":true`) || !strings.Contains(sessionResp.Body.String(), `"role":"admin"`) {
		t.Fatalf("unexpected admin session response: %d %s", sessionResp.Code, sessionResp.Body.String())
	}
	logoutResp := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/logout", "", cookie[0])
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logoutResp.Code, logoutResp.Body.String())
	}
	clearCookie := requireCookie(t, logoutResp.Result().Cookies(), standardSessionCookieName)
	if clearCookie.MaxAge >= 0 {
		t.Fatalf("expected logout to clear session cookie: %+v", clearCookie)
	}
	if response := serveAPIGet(router, "/api/v1/usage/overview", cookie[0]); response.Code != http.StatusUnauthorized {
		t.Fatalf("expected logged-out session to be rejected, got %d", response.Code)
	}

}

func TestAuthAPIKeyLoginSetsViewerSessionCookieAndSessionSummary(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	keyProvider := &authLoginKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-live123456", DisplayKey: "sk-l************3456", KeyAlias: "Team Key"}}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	loginResp := serveCredentialMutation(router, http.MethodPost, "/cpa/api/v1/auth/api-key-login", `{"apiKey":"sk-live123456"}`)
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

	sessionResp := serveAPIGet(router, "/cpa/api/v1/auth/session", cookies[0])

	body := sessionResp.Body.String()
	if sessionResp.Code != http.StatusOK || !strings.Contains(body, `"authenticated":true`) || !strings.Contains(body, `"role":"api_key_viewer"`) || !strings.Contains(body, `"api_key":{"display_key":"sk-*********123456","alias":"Team Key"}`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, body)
	}
	if strings.Contains(body, "sk-live123456") || strings.Contains(body, "sk-l************3456") {
		t.Fatalf("expected session response not to expose raw API key: %s", body)
	}
}

func TestAuthAPIKeyLoginFailuresAreGenericUnauthorized(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authLoginKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, body := range []string{`{"apiKey":"missing"}`, `{bad json}`} {
		resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/auth/api-key-login", body)
		if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "invalid credentials") {
			t.Fatalf("expected generic 401 for %s, got %d %s", body, resp.Code, resp.Body.String())
		}
	}
}

func TestAuthAPIKeyLoginSuccessClearsFailedAttempts(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authLoginKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for i := 0; i < 5-1; i++ {
		resp := performLoginRequest(router, "/api/v1/auth/api-key-login", `{"apiKey":"missing"}`, "198.51.100.11:1234")
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed attempt %d to return 401, got %d", i+1, resp.Code)
		}
	}

	keyProvider.findErr = nil
	keyProvider.row = entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}
	successResp := performLoginRequest(router, "/api/v1/auth/api-key-login", `{"apiKey":"sk-live"}`, "198.51.100.11:1234")
	if successResp.Code != http.StatusNoContent {
		t.Fatalf("expected successful API key login to be allowed and clear failed attempts, got %d %s", successResp.Code, successResp.Body.String())
	}

	keyProvider.findErr = context.Canceled
	resp := performLoginRequest(router, "/api/v1/auth/api-key-login", `{"apiKey":"missing"}`, "198.51.100.11:1234")

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
	keyProvider := &authLoginKeyStub{findErr: context.Canceled}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	resp := serveAPIGet(router, "/cpa/api/v1/auth/session", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected inactive session response: %d %s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive viewer session to be deleted")
	}
	cookies := resp.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != standardSessionCookieName || cookies[0].Path != "/cpa" || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected session cookie to be cleared, got %+v", cookies)
	}
}

func TestAuthRejectsExpiredSession(t *testing.T) {
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/v1/auth/session", http.StatusOK},
		{"/api/v1/usage/overview", http.StatusUnauthorized},
	} {
		t.Run(tc.path, func(t *testing.T) {
			sessions := auth.NewSessionManager(-time.Hour)
			token, _, err := sessions.CreateAPIKeyViewer(42)
			if err != nil {
				t.Fatalf("CreateAPIKeyViewer: %v", err)
			}
			config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: -time.Hour}
			router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")
			resp := serveAPIGet(router, tc.path, &http.Cookie{Name: standardSessionCookieName, Value: token})
			if resp.Code != tc.status || (tc.status == http.StatusOK && !strings.Contains(resp.Body.String(), `"authenticated":false`)) {
				t.Fatalf("expired session status=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestViewerSessionCannotAccessAdminManagementRoutes(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	keyProvider := &authLoginKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, path := range []string{"/api/v1/usage/api-keys", "/api/v1/usage/api-keys/settings", "/api/v1/auth/sessions"} {
		resp := serveAPIGet(router, path, &http.Cookie{Name: standardSessionCookieName, Value: token})

		if resp.Code != http.StatusForbidden {
			t.Fatalf("%s: expected viewer session to be forbidden from admin route, got %d %s", path, resp.Code, resp.Body.String())
		}
	}
}

func TestAuthSessionManagementListsAdminAndAPIKeySessionsWithCurrentFirst(t *testing.T) {
	sessions := auth.NewSessionManager(2 * time.Hour)
	adminToken1, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create admin 1 returned error: %v", err)
	}
	adminToken2, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create admin 2 returned error: %v", err)
	}
	viewerToken1, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer 42 returned error: %v", err)
	}
	viewerToken2, _, err := sessions.CreateAPIKeyViewer(43)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer 43 returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: 2 * time.Hour}
	keyProvider := &authLoginKeyStub{rowsByID: map[int64]entities.CPAAPIKey{
		42: {ID: 42, APIKey: "sk-live123456", DisplayKey: "legacy-display-key", KeyAlias: "Team Key"},
		43: {ID: 43, APIKey: "sk-other654321", DisplayKey: "legacy-other-key"},
	}}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	resp := serveAPIGet(router, "/api/v1/auth/sessions", &http.Cookie{Name: standardSessionCookieName, Value: adminToken1})

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, secret := range []string{"sk-live123456", "sk-other654321", "legacy-display-key", "legacy-other-key", adminToken1, adminToken2, viewerToken1, viewerToken2} {
		if strings.Contains(body, secret) {
			t.Fatalf("session management response leaked secret %q: %s", secret, body)
		}
	}
	var parsed struct {
		Items []struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			Role       string `json:"role"`
			Current    bool   `json:"current"`
			LoginAt    string `json:"loginAt"`
			ExpiresAt  string `json:"expiresAt"`
			APIKeyID   string `json:"apiKeyId"`
			Label      string `json:"label"`
			DisplayKey string `json:"displayKey"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Items) != 4 {
		t.Fatalf("expected two admin rows and two API key rows, got %+v", parsed.Items)
	}
	if parsed.Items[0].Kind != "admin" || !parsed.Items[0].Current {
		t.Fatalf("expected current admin session first, got %+v", parsed.Items)
	}

	var adminRows int
	apiLabels := map[string]string{}
	for _, item := range parsed.Items {
		if item.ID == "" || item.LoginAt == "" || item.ExpiresAt == "" {
			t.Fatalf("missing session metadata: %+v", item)
		}
		for _, value := range []string{item.LoginAt, item.ExpiresAt} {
			if _, err := time.Parse("2006/01/02 15:04:05", value); err != nil {
				t.Fatalf("invalid session time %q: %v", value, err)
			}
		}
		switch item.Kind {
		case "admin":
			adminRows++
			if item.Role != string(auth.RoleAdmin) {
				t.Fatalf("unexpected admin session row: %+v", item)
			}
		case "api_key":
			if item.Role != string(auth.RoleAPIKeyViewer) || item.APIKeyID == "" {
				t.Fatalf("unexpected API key session row: %+v", item)
			}
			apiLabels[item.APIKeyID] = item.Label + "\x00" + item.DisplayKey
		default:
			t.Fatalf("unexpected session item kind %q in %+v", item.Kind, item)
		}
	}
	if adminRows != 2 {
		t.Fatalf("expected two admin rows, got %d in %+v", adminRows, parsed.Items)
	}
	if apiLabels["42"] != "Team Key\x00sk-*********123456" {
		t.Fatalf("expected API key 42 to use alias and canonical mask, got %q", apiLabels["42"])
	}
	if apiLabels["43"] != "sk-*********654321\x00sk-*********654321" {
		t.Fatalf("expected API key 43 to fall back to masked key, got %q", apiLabels["43"])
	}
}

func TestAuthSessionManagementRevokesCurrentAdminSession(t *testing.T) {
	sessions := auth.NewSessionManager(2 * time.Hour)
	adminToken1, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create admin 1 returned error: %v", err)
	}
	adminToken2, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create admin 2 returned error: %v", err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: 2 * time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	listResp := serveAPIGet(router, "/api/v1/auth/sessions", &http.Cookie{Name: standardSessionCookieName, Value: adminToken1})
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d body=%s", listResp.Code, listResp.Body.String())
	}
	var parsed struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Items) == 0 || !parsed.Items[0].Current || parsed.Items[0].ID == "" {
		t.Fatalf("expected current session first in list response, got %+v", parsed.Items)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/"+parsed.Items[0].ID, nil)
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: adminToken1})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(adminToken1) {
		t.Fatal("expected current admin session to be invalid after managed session logout")
	}
	if !sessions.Validate(adminToken2) {
		t.Fatal("expected other admin sessions to remain valid after current session logout")
	}
	if !sessions.Validate(viewerToken) {
		t.Fatal("expected API key viewer session to remain valid after current session logout")
	}

	usageResp := serveAPIGet(router, "/api/v1/usage/overview", &http.Cookie{Name: standardSessionCookieName, Value: adminToken1})
	if usageResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked current admin session to be rejected, got %d %s", usageResp.Code, usageResp.Body.String())
	}
	clearCookies := resp.Result().Cookies()
	if len(clearCookies) == 0 || clearCookies[0].Name != standardSessionCookieName || clearCookies[0].MaxAge >= 0 {
		t.Fatalf("expected current managed session logout to clear current session cookie, got %+v", clearCookies)
	}
}

func TestAdminSessionCannotAccessKeyOverviewRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	resp := serveAPIGet(router, "/api/v1/key-overview?range=24h", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected admin session to be forbidden from key overview route, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthLoginRateLimitsRepeatedFailures(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	for i := 0; i < 5; i++ {
		resp := performLoginRequest(router, "/api/v1/auth/login", `{"password":"wrong"}`, "198.51.100.1:1234")
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed attempt %d to return 401, got %d", i+1, resp.Code)
		}
	}

	resp := performLoginRequest(router, "/api/v1/auth/login", `{"password":"wrong"}`, "198.51.100.1:1234")

	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeated failed attempts to return 429, got %d", resp.Code)
	}
}

func TestAuthLogoutClearsSecureSessionCookieBehindHTTPSProxy(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "")

	loginResp := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	loginReq.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("X-Forwarded-Proto", "https")
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusNoContent {
		t.Fatalf("expected login status 204, got %d", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure {
		t.Fatalf("expected proxied HTTPS login to set a secure session cookie, got %+v", cookies)
	}

	logoutResp := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	logoutReq.Header.Set("X-Forwarded-Proto", "https")
	logoutReq.AddCookie(cookies[0])
	router.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("expected logout status 204, got %d", logoutResp.Code)
	}
	clearCookies := logoutResp.Result().Cookies()
	if len(clearCookies) == 0 || clearCookies[0].Name != standardSessionCookieName || clearCookies[0].MaxAge >= 0 || !clearCookies[0].Secure {
		t.Fatalf("expected proxied HTTPS logout to clear a secure session cookie, got %+v", clearCookies)
	}
}

func TestSubpathAuthUsesPrefixedRoutesAndCookiePath(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "/cpa")

	sessionResp := serveAPIGet(router, "/cpa/api/v1/auth/session")
	if sessionResp.Code != http.StatusOK || !strings.Contains(sessionResp.Body.String(), `"authenticated":false`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, sessionResp.Body.String())
	}

	loginResp := serveCredentialMutation(router, http.MethodPost, "/cpa/api/v1/auth/login", `{"password":"secret"}`)
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

	usageResp := serveAPIGet(router, "/cpa/api/v1/usage/overview", cookies[0])
	if usageResp.Code != http.StatusOK {
		t.Fatalf("expected protected route under subpath to succeed, got %d %s", usageResp.Code, usageResp.Body.String())
	}

	unprefixedResp := serveAPIGet(router, "/api/v1/usage/overview", cookies[0])
	if unprefixedResp.Code != http.StatusNotFound {
		t.Fatalf("expected unprefixed route to 404, got %d", unprefixedResp.Code)
	}
}
