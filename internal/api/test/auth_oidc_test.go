package test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/oidc"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

type oidcAPIEnvironment struct {
	router         http.Handler
	sessions       *auth.SessionManager
	idp            *httptest.Server
	base, public   string
	mu             sync.Mutex
	authorizations map[string]url.Values
	failurePath    string
	failureStatus  int
	failureCode    string
}

func newOIDCAPIEnvironment(t *testing.T, base, scheme string) *oidcAPIEnvironment {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	e := &oidcAPIEnvironment{base: base, public: strings.ToLower(scheme) + "://keeper.example", authorizations: make(map[string]url.Values)}
	e.idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		e.mu.Lock()
		failurePath, failureStatus, failureCode := e.failurePath, e.failureStatus, e.failureCode
		e.mu.Unlock()
		if r.URL.Path == failurePath {
			w.WriteHeader(failureStatus)
			if failureStatus == http.StatusOK {
				_, _ = w.Write([]byte(`{"keys":`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": failureCode, "error_description": "upstream-secret"})
			return
		}
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": e.idp.URL, "authorization_endpoint": e.idp.URL + "/authorize", "token_endpoint": e.idp.URL + "/token", "jwks_uri": e.idp.URL + "/keys", "response_types_supported": []string{"code"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "key", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			_ = r.ParseForm()
			client, secret, _ := r.BasicAuth()
			if client != "keeper" || secret != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
			e.mu.Lock()
			query := e.authorizations[r.Form.Get("code")]
			delete(e.authorizations, r.Form.Get("code"))
			e.mu.Unlock()
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if query == nil || query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"upstream-secret"}`))
				return
			}
			claims, _ := json.Marshal(map[string]any{"iss": e.idp.URL, "sub": "Subject", "aud": "keeper", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": query.Get("nonce"), "name": "Admin display"})
			input := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
			digest := sha256.Sum256([]byte(input))
			signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			_ = json.NewEncoder(w).Encode(map[string]any{"id_token": input + "." + base64.RawURLEncoding.EncodeToString(signature), "access_token": "access", "token_type": "Bearer"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(e.idp.Close)
	client, err := oidc.NewClient(oidc.Config{Issuer: e.idp.URL, ClientID: "keeper", ClientSecret: "secret", RedirectURL: scheme + "://keeper.example" + base + "/api/v1/auth/oidc/callback"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.sessions = auth.NewSessionManager(time.Hour)
	cfg := keeperapi.AuthConfig{Enabled: true, LoginPassword: "password", SessionTTL: time.Hour, BasePath: base, OIDC: client}
	e.router = keeperapi.NewRouter(nil, nil, nil, nil, cfg, keeperapi.NewAuthHandler(cfg, e.sessions), base)
	return e
}

func (e *oidcAPIEnvironment) failEndpoint(path string, status int, code string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failurePath, e.failureStatus, e.failureCode = path, status, code
}

func (e *oidcAPIEnvironment) request(method, target, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, e.public+target, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.2:1234"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	e.router.ServeHTTP(resp, req)
	return resp
}

func (e *oidcAPIEnvironment) begin(t *testing.T, target string) (url.Values, *http.Cookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"return_to": target})
	response := e.request(http.MethodPost, e.base+"/api/v1/auth/oidc/login", string(body), nil, nil)
	if response.Code != 200 {
		t.Fatalf("begin: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatal("invalid login response")
	}
	u, _ := url.Parse(payload.AuthorizationURL)
	query := u.Query()
	e.mu.Lock()
	e.authorizations["code-"+query.Get("state")] = query
	e.mu.Unlock()
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login response is cacheable")
	}
	return query, response.Result().Cookies()[0]
}

func TestOIDCAPICompleteLoginCookieSourcesAndReplay(t *testing.T) {
	for _, scheme := range []string{"http", "https", "HTTPS"} {
		t.Run(scheme, func(t *testing.T) {
			e := newOIDCAPIEnvironment(t, "/cpa", scheme)
			target := "/cpa/auth-files?credential=abc&provider=codex#detail"
			query, binding := e.begin(t, target)
			if binding.Secure != strings.EqualFold(scheme, "https") || !binding.HttpOnly || binding.SameSite != http.SameSiteLaxMode || binding.Path != "/cpa/api/v1/auth/oidc" {
				t.Fatalf("wrong binding cookie: %+v", binding)
			}
			callback := e.base + "/api/v1/auth/oidc/callback?state=" + query.Get("state") + "&code=code-" + query.Get("state")
			otherBrowser := e.request(http.MethodGet, callback, "", nil, nil)
			if !strings.Contains(otherBrowser.Header().Get("Location"), "oidc_failed") {
				t.Fatalf("cross-browser callback accepted: %d %s", otherBrowser.Code, otherBrowser.Body.String())
			}
			response := e.request(http.MethodGet, callback, "", []*http.Cookie{binding}, nil)
			if response.Code != 303 || response.Header().Get("Location") != target {
				t.Fatalf("callback: %d %s", response.Code, response.Header().Get("Location"))
			}
			var sessionCookie *http.Cookie
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == standardSessionCookieName {
					sessionCookie = cookie
				}
			}
			if sessionCookie == nil || sessionCookie.Secure != strings.EqualFold(scheme, "https") || !sessionCookie.HttpOnly || sessionCookie.Path != "/cpa" {
				t.Fatalf("wrong session cookie: %+v", sessionCookie)
			}
			session, ok := e.sessions.Get(sessionCookie.Value)
			if !ok || session.Source != auth.SessionSourceOIDC || session.Role != auth.RoleAdmin || session.OIDCSubject != "Subject" || session.Alias != "Admin display" {
				t.Fatalf("wrong session: %+v %v", session, ok)
			}
			capabilities := e.request(http.MethodGet, e.base+"/api/v1/auth/session", "", []*http.Cookie{sessionCookie}, nil)
			if !strings.Contains(capabilities.Body.String(), `"authenticated":true`) || !strings.Contains(capabilities.Body.String(), `"oidc_login_enabled":true`) {
				t.Fatal(capabilities.Body.String())
			}
			embedCookie := *sessionCookie
			embedCookie.Name = embedSessionCookieName
			embed := e.request(http.MethodGet, e.base+"/api/v1/auth/session", "", []*http.Cookie{&embedCookie}, map[string]string{embedHeaderName: "cpamc"})
			if !strings.Contains(embed.Body.String(), `"authenticated":false`) || !strings.Contains(embed.Body.String(), `"oidc_login_enabled":false`) {
				t.Fatalf("OIDC crossed embed boundary: %s", embed.Body.String())
			}
			replay := e.request(http.MethodGet, callback, "", []*http.Cookie{binding}, nil)
			if !strings.Contains(replay.Header().Get("Location"), "oidc_expired") || len(e.sessions.List()) != 1 {
				t.Fatal("callback replay created a session")
			}
		})
	}
}

func TestOIDCAPIOriginReturnToAndRequestIntentBoundaries(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "/cpa", "http")
	for index, target := range []string{"https://evil.example/", "//evil.example", "/other", "/cpa/../other", "/cpa\\evil", "/cpa/%2f%2fevil", "/cpa/%5cevil", "/cpa/?embed=cpamc", "/cpa/?mode=cpamc", "/cpa/api/v1/auth/logout", "/cpa/?x=%0d%0aLocation%3aevil"} {
		body, _ := json.Marshal(map[string]string{"return_to": target})
		req := httptest.NewRequest(http.MethodPost, e.public+"/cpa/api/v1/auth/oidc/login", strings.NewReader(string(body)))
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", index+20)
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		response := httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("unsafe target accepted: %q %d", target, response.Code)
		}
	}
	for name, headers := range map[string]map[string]string{
		"request intent": {requestIntentHeaderName: ""}, "embed": {embedHeaderName: "cpamc"}, "iframe": {"Sec-Fetch-Dest": "iframe"}, "foreign origin": {"Origin": "https://evil.example"},
	} {
		t.Run(name, func(t *testing.T) {
			response := e.request(http.MethodPost, "/cpa/api/v1/auth/oidc/login", `{}`, nil, headers)
			if response.Code < 400 {
				t.Fatalf("boundary accepted: %d", response.Code)
			}
		})
	}
}

func TestOIDCAPIPublicHTTPSProxyAndUntrustedForwardedHeaders(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "https")
	for _, trusted := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "http://backend.internal/api/v1/auth/oidc/login", strings.NewReader(`{}`))
		req.RemoteAddr = "192.0.2.2:12"
		if trusted {
			req.RemoteAddr = "127.0.0.1:12"
		}
		req.Header.Set("X-Forwarded-Host", "keeper.example")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		response := httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if trusted && response.Code != 200 || !trusted && response.Code != 400 {
			t.Fatalf("trusted=%v status=%d body=%s", trusted, response.Code, response.Body.String())
		}
		if trusted && !response.Result().Cookies()[0].Secure {
			t.Fatal("public HTTPS proxy lost Secure binding")
		}
	}
}

func TestOIDCAPIOriginDiagnosticOmitsSensitiveQueryAndHeaders(t *testing.T) {
	originalHooks := logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	hook := logtest.NewGlobal()
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(originalHooks) })
	e := newOIDCAPIEnvironment(t, "", "https")
	for _, trusted := range []bool{false, true} {
		hook.Reset()
		req := httptest.NewRequest(http.MethodPost, "http://backend.internal/api/v1/auth/oidc/login?code=private-code", strings.NewReader(`{}`))
		req.RemoteAddr = "192.0.2.2:12"
		wantOrigin := "http://backend.internal"
		if trusted {
			req.RemoteAddr = "127.0.0.1:12"
			wantOrigin = "https://wrong.example"
		}
		req.Header.Set("X-Forwarded-Host", "wrong.example")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("Authorization", "Bearer private-token")
		req.Header.Set("Origin", "https://browser.example/path?secret=private-query")
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		response := httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "oidc_public_origin") {
			t.Fatalf("origin failure response changed: %d %s", response.Code, response.Body.String())
		}
		entries := hook.AllEntries()
		if len(entries) != 1 {
			t.Fatalf("origin mismatch produced %d logs", len(entries))
		}
		fields := entries[0].Data
		if fields["expected_origin"] != "https://keeper.example" || fields["actual_origin"] != wantOrigin || fields["browser_origin"] != "https://browser.example" || fields["trusted_proxy"] != trusted {
			t.Fatalf("origin diagnostic = %+v", fields)
		}
		encoded, _ := json.Marshal(fields)
		for _, secret := range []string{"private-code", "private-token", "private-query"} {
			if strings.Contains(string(encoded), secret) || strings.Contains(response.Body.String(), secret) {
				t.Fatalf("sensitive value leaked: %s", secret)
			}
		}
	}
}

func TestOIDCAPIEquivalentIPv6OriginsPreserveRedirectURL(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	for _, tc := range []struct {
		name, configured, actual, origin string
		allowed                          bool
	}{
		{"expanded", "http://[0:0:0:0:0:0:0:1]", "http://[::1]", "http://[::1]", true},
		{"hex-case", "http://[2001:0DB8:0:0:0:0:0:1]:8080", "http://[2001:db8::1]:8080", "http://[2001:db8::1]:8080", true},
		{"default-port", "https://[0:0:0:0:0:0:0:1]:443", "https://[::1]", "https://[::1]", true},
		{"mapped-notation", "http://[::ffff:192.0.2.1]", "http://[::ffff:c000:201]", "http://[::ffff:c000:201]", true},
		{"mapped-is-not-ipv4", "http://[::ffff:192.0.2.1]", "http://192.0.2.1", "http://192.0.2.1", false},
		{"different-ip", "http://[::1]", "http://[::2]", "http://[::2]", false},
		{"different-port", "http://[::1]:8080", "http://[::1]:8081", "http://[::1]:8081", false},
		{"different-scheme", "https://[::1]", "http://[::1]", "http://[::1]", false},
		{"foreign-origin", "http://[::1]", "http://[::1]", "http://[::2]", false},
		{"ipv4", "http://192.0.2.1:80", "http://192.0.2.1", "http://192.0.2.1", true},
		{"hostname", "https://KEEPER.example:443", "https://keeper.example", "https://keeper.example", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirect := tc.configured + "/api/v1/auth/oidc/callback"
			client, err := oidc.NewClient(oidc.Config{Issuer: e.idp.URL, ClientID: "keeper", ClientSecret: "secret", RedirectURL: redirect}, nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg := keeperapi.AuthConfig{Enabled: true, LoginPassword: "password", OIDC: client}
			router := keeperapi.NewRouter(nil, nil, nil, nil, cfg, keeperapi.NewAuthHandler(cfg, auth.NewSessionManager(time.Hour)), "")
			req := httptest.NewRequest(http.MethodPost, tc.actual+"/api/v1/auth/oidc/login", strings.NewReader(`{}`))
			req.RemoteAddr = "192.0.2.2:1234"
			req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if !tc.allowed {
				if response.Code != 400 || !strings.Contains(response.Body.String(), "oidc_public_origin") {
					t.Fatalf("foreign origin accepted: %d %s", response.Code, response.Body.String())
				}
				return
			}
			if response.Code != 200 {
				t.Fatalf("equivalent origin rejected: %d %s", response.Code, response.Body.String())
			}
			var payload struct {
				URL string `json:"authorization_url"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			authorization, err := url.Parse(payload.URL)
			if err != nil {
				t.Fatal(err)
			}
			if authorization.Query().Get("redirect_uri") != redirect {
				t.Fatal("origin comparison rewrote the registered redirect URI")
			}
		})
	}
}

func TestOIDCAPIBrowserCapacityUsesAllExistingBindingCookies(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	loginURL, _ := url.Parse(e.public + "/api/v1/auth/oidc/login")
	for index := range 9 {
		req := httptest.NewRequest(http.MethodPost, e.public+"/api/v1/auth/oidc/login", strings.NewReader(`{}`))
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:12", index+1)
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		for _, cookie := range jar.Cookies(loginURL) {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if index < 8 {
			if response.Code != 200 {
				t.Fatalf("transaction %d: %d %s", index, response.Code, response.Body.String())
			}
			jar.SetCookies(loginURL, response.Result().Cookies())
		} else if response.Code != 429 {
			t.Fatalf("browser capacity not enforced: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestOIDCAPISuccessResetsOriginalLoginSourceAfterCallbackIPChanges(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	for range 7 {
		// 先留下失败记录，才能验证成功回调确实清零了发起地址的计数。
		failed, failedBinding := e.begin(t, "/")
		failure := "/api/v1/auth/oidc/callback?" + url.Values{"state": {failed.Get("state")}, "code": {"invalid-code"}}.Encode()
		response := e.request(http.MethodGet, failure, "", []*http.Cookie{failedBinding}, nil)
		if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "oidc_error=") {
			t.Fatalf("invalid code callback = %d", response.Code)
		}
		query, binding := e.begin(t, "/")
		req := httptest.NewRequest(http.MethodGet, e.public+"/api/v1/auth/oidc/callback?state="+query.Get("state")+"&code=code-"+query.Get("state"), nil)
		req.RemoteAddr = "192.0.2.99:1234"
		req.AddCookie(binding)
		response = httptest.NewRecorder()
		e.router.ServeHTTP(response, req)
		if response.Code != 303 || response.Header().Get("Location") != "/" {
			t.Fatalf("successful login did not reset original source failures: %d %s", response.Code, response.Header().Get("Location"))
		}
	}
}

func TestOIDCAPIProviderErrorAndUnknownStatePreserveExistingSessions(t *testing.T) {
	e := newOIDCAPIEnvironment(t, "", "http")
	token, _, err := e.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	query, binding := e.begin(t, "/settings?tab=sessions")
	response := e.request(http.MethodGet, "/api/v1/auth/oidc/callback?state="+query.Get("state")+"&error=access_denied&error_description=upstream-secret", "", []*http.Cookie{binding, {Name: standardSessionCookieName, Value: token}}, nil)
	if !strings.Contains(response.Header().Get("Location"), "oidc_canceled") || strings.Contains(response.Body.String(), "upstream-secret") || !e.sessions.Validate(token) {
		t.Fatalf("unsafe provider error: %d %s", response.Code, response.Body.String())
	}
	unknown := e.request(http.MethodGet, "/api/v1/auth/oidc/callback?state=unknown&code=sensitive-code", "", []*http.Cookie{{Name: standardSessionCookieName, Value: token}}, nil)
	if !e.sessions.Validate(token) || strings.Contains(unknown.Body.String(), "sensitive-code") || unknown.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unknown callback changed existing login or leaked query")
	}
}
