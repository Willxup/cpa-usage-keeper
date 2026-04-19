package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
)

func TestAuthSessionReportsAuthenticatedWhenDisabled(t *testing.T) {
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{Enabled: false}, nil)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !contains(resp.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected response: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAuthProtectedRouteRequiresSessionWhenEnabled(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, NewAuthHandler(AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, sessions))
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestAuthLoginSetsCookieAndUnlocksProtectedRoute(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	handler := NewAuthHandler(AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, sessions)
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, handler)

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

	usageResp := httptest.NewRecorder()
	usageReq := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	usageReq.AddCookie(cookie[0])
	router.ServeHTTP(usageResp, usageReq)

	if usageResp.Code != http.StatusOK {
		t.Fatalf("expected protected route to succeed, got %d %s", usageResp.Code, usageResp.Body.String())
	}
}

func TestAuthLoginRejectsWrongPassword(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, NewAuthHandler(AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, sessions))
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}
