package test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOIDCCancellationAndProviderFailuresDoNotConsumeFailureAllowance(t *testing.T) {
	for _, scenario := range []struct {
		name, path, code string
		status           int
	}{
		{name: "canceled", code: "access_denied"},
		{name: "provider error", code: "server_error"},
		{name: "provider busy", code: "temporarily_unavailable"},
		{name: "discovery unavailable", path: "/.well-known/openid-configuration", status: 503, code: "server_error"},
		{name: "token unavailable", path: "/token", status: 503, code: "server_error"},
		{name: "token throttled", path: "/token", status: 429, code: "invalid_grant"},
		{name: "token server failure", path: "/token", status: 503, code: "invalid_grant"},
		{name: "client misconfigured", path: "/token", status: 401, code: "invalid_client"},
		{name: "keys unavailable", path: "/keys", status: 503, code: "server_error"},
		{name: "keys malformed", path: "/keys", status: 200, code: "server_error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			e := newOIDCAPIEnvironment(t, "", "http")
			e.failEndpoint(scenario.path, scenario.status, scenario.code)
			for range 7 {
				if scenario.path == "/.well-known/openid-configuration" {
					response := e.request(http.MethodPost, "/api/v1/auth/oidc/login", `{}`, nil, nil)
					if response.Code != 503 {
						t.Fatalf("discovery failure consumed allowance: %d", response.Code)
					}
					continue
				}
				query, binding := e.begin(t, "/")
				callback := url.Values{"state": {query.Get("state")}}
				if scenario.path == "" {
					callback.Set("error", scenario.code)
				} else {
					callback.Set("code", "code-"+query.Get("state"))
				}
				response := e.request(http.MethodGet, "/api/v1/auth/oidc/callback?"+callback.Encode(), "", []*http.Cookie{binding}, nil)
				if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "oidc_error=") {
					t.Fatalf("failed callback = %d %s", response.Code, response.Header().Get("Location"))
				}
			}
		})
	}
}

func TestOIDCFailuresCountOnceForOriginalSourceAndCancellationDoesNotReset(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	// 从未成功登录时也必须累计真实的 invalid_grant，不能被认证方式探测覆盖。
	for failure := range 5 {
		query, binding := e.begin(t, "/")
		callback := "/api/v1/auth/oidc/callback?" + url.Values{"state": {query.Get("state")}, "code": {"invalid-code"}}.Encode()
		req := httptest.NewRequest(http.MethodGet, e.public+callback, nil)
		req.RemoteAddr = "192.0.2.99:1234"
		req.AddCookie(binding)
		response := httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "oidc_error=") {
			t.Fatalf("invalid code callback = %d", response.Code)
		}
		if failure == 0 {
			for range 6 {
				e.request(http.MethodGet, callback, "", []*http.Cookie{binding}, nil)
			}
		}
		if failure == 3 {
			for range 6 {
				query, binding := e.begin(t, "/")
				e.request(http.MethodGet, "/api/v1/auth/oidc/callback?"+url.Values{"state": {query.Get("state")}, "error": {"access_denied"}}.Encode(), "", []*http.Cookie{binding}, nil)
			}
		}
	}
	response := e.request(http.MethodPost, "/api/v1/auth/oidc/login", `{}`, nil, nil)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("confirmed failures did not limit original source: %d", response.Code)
	}
	password := e.request(http.MethodPost, "/api/v1/auth/login", `{"password":"password"}`, nil, nil)
	if password.Code != 204 && password.Code != 200 {
		t.Fatalf("OIDC source failures blocked password fallback: %d", password.Code)
	}
}

func TestOIDCUnboundCallbackCannotChargeVictimAndGlobalRequestLimitRemains(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	for range 60 {
		query, binding := e.begin(t, "/")
		callback := "/api/v1/auth/oidc/callback?" + url.Values{"state": {query.Get("state")}, "error": {"invalid_response"}}.Encode()
		e.request(http.MethodGet, callback, "", nil, nil)
		callback = "/api/v1/auth/oidc/callback?" + url.Values{"state": {query.Get("state")}, "error": {"access_denied"}}.Encode()
		e.request(http.MethodGet, callback, "", []*http.Cookie{binding}, nil)
	}
	response := e.request(http.MethodPost, "/api/v1/auth/oidc/login", `{}`, nil, nil)
	if response.Code != 429 {
		t.Fatalf("global request limit no longer applies to canceled logins: %d", response.Code)
	}
}
