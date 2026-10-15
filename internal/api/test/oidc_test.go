package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	oidcrp "cpa-usage-keeper/internal/oidc"
	"github.com/gin-gonic/gin"
)

// fakeRP is an in-memory OIDCRPClient that issues a fixed identity.
type fakeRP struct {
	loginURL    string
	callbackErr error
	identity    oidcrp.Identity
	lastState   string
	lastCode    string
}

func (f *fakeRP) Login(context.Context) (*oidcrp.LoginResult, error) {
	return &oidcrp.LoginResult{AuthorizationURL: f.loginURL}, nil
}

func (f *fakeRP) Callback(_ context.Context, state, code string) (*oidcrp.CallbackResult, error) {
	f.lastState = state
	f.lastCode = code
	if f.callbackErr != nil {
		return nil, f.callbackErr
	}
	return &oidcrp.CallbackResult{Identity: f.identity}, nil
}

func newOIDCTestRouter(t *testing.T, rp OIDCRPClient) (*gin.Engine, *auth.SessionManager) {
	t.Helper()
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{
		Enabled:       true,
		LoginPassword: "secret",
		SessionTTL:    time.Hour,
		OIDCEnabled:   true,
		OIDCRP:        rp,
	}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")
	return router, sessions
}

func serveOIDCGet(router *gin.Engine, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestOIDCLoginRedirectsToIdP(t *testing.T) {
	rp := &fakeRP{loginURL: "https://idp.example.com/authorize?state=st"}
	router, _ := newOIDCTestRouter(t, rp)

	resp := serveOIDCGet(router, "/api/v1/auth/oidc/login")
	if resp.Code != http.StatusFound {
		t.Fatalf("expected oidc login redirect 302, got %d", resp.Code)
	}
	if location := resp.Header().Get("Location"); location != rp.loginURL {
		t.Fatalf("expected redirect to %q, got %q", rp.loginURL, location)
	}
}

func TestOIDCCallbackCreatesAdminSessionWithOIDCSource(t *testing.T) {
	rp := &fakeRP{identity: oidcrp.Identity{Subject: "user-123", Email: "user@example.com", Name: "Smoke User"}}
	router, sessions := newOIDCTestRouter(t, rp)

	resp := serveOIDCGet(router, "/api/v1/auth/oidc/callback?code=test-code&state=test-state")
	if rp.lastCode != "test-code" || rp.lastState != "test-state" {
		t.Fatalf("expected RP to receive code/state, got %q/%q", rp.lastCode, rp.lastState)
	}
	if resp.Code != http.StatusFound {
		t.Fatalf("expected callback success redirect 302, got %d %s", resp.Code, resp.Body.String())
	}
	if location := resp.Header().Get("Location"); location != "/" {
		t.Fatalf("expected success redirect to /, got %q", location)
	}

	cookies := resp.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected callback to set session cookie")
	}
	sessionResp := serveOIDCGet(router, "/api/v1/auth/session", cookies[0])
	if sessionResp.Code != http.StatusOK || !containsAll(sessionResp.Body.String(), `"authenticated":true`, `"role":"admin`, `"oidc_login_enabled":true`) {
		t.Fatalf("unexpected session response: %d %s", sessionResp.Code, sessionResp.Body.String())
	}

	records := sessions.List()
	if len(records) != 1 {
		t.Fatalf("expected exactly one session, got %d", len(records))
	}
	if records[0].Source != auth.SessionSourceOIDC {
		t.Fatalf("expected session source %q, got %q", auth.SessionSourceOIDC, records[0].Source)
	}
	if records[0].Alias != "Smoke User" {
		t.Fatalf("expected session alias Smoke User, got %q", records[0].Alias)
	}
	if records[0].Role != auth.RoleAdmin {
		t.Fatalf("expected admin role, got %q", records[0].Role)
	}
}

func TestOIDCCallbackFailureRedirectsWithErrorHint(t *testing.T) {
	rp := &fakeRP{callbackErr: context.Canceled}
	router, _ := newOIDCTestRouter(t, rp)

	resp := serveOIDCGet(router, "/api/v1/auth/oidc/callback?code=bad&state=stale")
	if resp.Code != http.StatusFound {
		t.Fatalf("expected failure redirect 302, got %d", resp.Code)
	}
	if location := resp.Header().Get("Location"); location != "/?oidc_error=login_failed" {
		t.Fatalf("expected error redirect, got %q", location)
	}
}

func TestOIDCRoutesInactiveWhenDisabled(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, nil, nil, config, handler, "")

	resp := serveOIDCGet(router, "/api/v1/auth/oidc/login")
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected disabled oidc login 204, got %d", resp.Code)
	}
	sessionResp := serveOIDCGet(router, "/api/v1/auth/session")
	if containsAll(sessionResp.Body.String(), "oidc_login_enabled") {
		t.Fatalf("expected no oidc flag when disabled, got %s", sessionResp.Body.String())
	}
}

func containsAll(body string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			return false
		}
	}
	return true
}
