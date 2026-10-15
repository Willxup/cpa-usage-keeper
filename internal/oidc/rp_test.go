// OIDC RP end-to-end test against an in-process mock IdP: discovery, authorize,
// PKCE token exchange, ID-token verification and single-use state handling.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// mockIdP implements the minimal OIDC provider surface: discovery, authorize, token.
type mockIdP struct {
	baseURL  string
	key      *rsa.PrivateKey
	mu       sync.Mutex
	codes    map[string]string // code -> subject
	lastCode string
	// emailVerified controls the email_verified claim: nil omits it entirely.
	emailVerified any
}

func newMockIdP(t *testing.T) *mockIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return &mockIdP{key: key, codes: make(map[string]string)}
}

func (m *mockIdP) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"],
			"code_challenge_methods_supported": ["S256"]
		}`, m.baseURL, m.baseURL+"/authorize", m.baseURL+"/token", m.baseURL+"/jwks")
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(m.key.N.Bytes())
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"keys":[{"kty":"RSA","alg":"RS256","use":"sig","kid":"smoke-key-1","n":%q,"e":"AQAB"}]}`, n)
	})

	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		// Mock IdP immediately grants a code for the requested user; real flow
		// would authenticate the user here. PKCE params are recorded by the RP.
		code := fmt.Sprintf("code-%d", time.Now().UnixNano())
		m.mu.Lock()
		m.codes[code] = "user-123"
		m.mu.Unlock()
		redirect := r.URL.Query().Get("redirect_uri") + "?code=" + code + "&state=" + r.URL.Query().Get("state")
		http.Redirect(w, r, redirect, http.StatusFound)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		code := r.PostForm.Get("code")
		m.mu.Lock()
		subject, ok := m.codes[code]
		if ok {
			delete(m.codes, code)
		}
		m.mu.Unlock()
		if !ok {
			http.Error(w, "unknown code", http.StatusBadRequest)
			return
		}
		now := time.Now()
		header := map[string]any{"typ": "JWT", "alg": string(jose.RS256), "kid": "smoke-key-1"}
		payload := map[string]any{
			"iss":   m.baseURL,
			"sub":   subject,
			"aud":   "smoke-client",
			"exp":   now.Add(10 * time.Minute).Unix(),
			"iat":   now.Unix(),
			"email": "user@example.com",
			"name":  "Smoke User",
		}
		m.mu.Lock()
		if m.emailVerified != nil {
			payload["email_verified"] = m.emailVerified
		}
		m.mu.Unlock()
		signed, err := signIDToken(m.key, header, payload)
		if err != nil {
			t.Errorf("sign id token: %v", err)
			http.Error(w, "signing failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"at","token_type":"Bearer","expires_in":3600,"id_token":%q}`, signed)
	})

	return mux
}

func TestRPFullFlow(t *testing.T) {
	idp := newMockIdP(t)
	server := httptest.NewServer(idp.handler(t))
	defer server.Close()
	idp.baseURL = server.URL
	idp.emailVerified = true

	rp := NewRP(Config{
		Issuer:       server.URL,
		ClientID:     "smoke-client",
		ClientSecret: "smoke-secret",
		RedirectURL:  "https://keeper.example.com/api/v1/auth/oidc/callback",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Start login: get authorization URL with state + PKCE challenge.
	login, err := rp.Login(ctx)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.HasPrefix(login.AuthorizationURL, server.URL+"/authorize") {
		t.Fatalf("authorization URL should point at IdP: %s", login.AuthorizationURL)
	}

	// 2. Simulate the IdP issuing a code (parse state from the auth URL).
	authURLParts := strings.SplitN(login.AuthorizationURL, "?", 2)
	if len(authURLParts) != 2 {
		t.Fatalf("authorization URL missing query: %s", login.AuthorizationURL)
	}
	query := authURLParts[1]
	state := extractQueryParam(t, query, "state")
	challenge := extractQueryParam(t, query, "code_challenge")
	if len(challenge) < 40 {
		t.Fatalf("PKCE challenge missing or too short: %q", challenge)
	}
	if method := extractQueryParam(t, query, "code_challenge_method"); method != "S256" {
		t.Fatalf("expected S256 PKCE method, got %q", method)
	}

	// 3. Mock IdP issues the code for our state.
	idp.mu.Lock()
	code := fmt.Sprintf("smoke-code-%d", time.Now().UnixNano())
	idp.codes[code] = "user-123"
	idp.mu.Unlock()

	// 4. Complete callback: token exchange + ID token verification.
	result, err := rp.Callback(ctx, state, code)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if result.Identity.Subject != "user-123" {
		t.Fatalf("subject = %q, want user-123", result.Identity.Subject)
	}
	if result.Identity.Email != "user@example.com" {
		t.Fatalf("email = %q, want user@example.com", result.Identity.Email)
	}
	if !result.Identity.EmailVerified {
		t.Fatal("email_verified = false, want true")
	}
	if result.Identity.Name != "Smoke User" {
		t.Fatalf("name = %q, want Smoke User", result.Identity.Name)
	}

	// 5. State replay must fail (single-use).
	if _, err := rp.Callback(ctx, state, code); err == nil {
		t.Fatal("replaying consumed state+code must fail")
	}

	// 6. Unknown state must fail.
	if _, err := rp.Callback(ctx, "bogus-state", code); err == nil {
		t.Fatal("unknown state must fail")
	}
}

func extractQueryParam(t *testing.T, query, key string) string {
	t.Helper()
	for _, pair := range strings.Split(query, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

// startMockIdP runs the handler in-process and points the mock at it.
func startMockIdP(t *testing.T, idp *mockIdP) string {
	t.Helper()
	server := httptest.NewServer(idp.handler(t))
	t.Cleanup(server.Close)
	idp.baseURL = server.URL
	return server.URL
}

// loginAndCallback drives Login + code issuance and returns the Callback outcome.
func loginAndCallback(t *testing.T, rp *RP, idp *mockIdP) (*CallbackResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	login, err := rp.Login(ctx)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	query := strings.SplitN(login.AuthorizationURL, "?", 2)[1]
	state := extractQueryParam(t, query, "state")
	idp.mu.Lock()
	code := fmt.Sprintf("allowlist-code-%d", time.Now().UnixNano())
	idp.codes[code] = "user-123"
	idp.mu.Unlock()
	return rp.Callback(ctx, state, code)
}

// issueResult issues a callback for user-123 with the given allowlist; the caller
// configures idp.emailVerified beforehand.
func issueResult(t *testing.T, idp *mockIdP, allowed []string) (*CallbackResult, error) {
	rp := NewRP(Config{
		Issuer:       startMockIdP(t, idp),
		ClientID:     "smoke-client",
		ClientSecret: "smoke-secret",
		RedirectURL:  "https://keeper.example.com/api/v1/auth/oidc/callback",
		AllowedUsers: allowed,
	})
	return loginAndCallback(t, rp, idp)
}

// TestRPCallbackAllowlistRequiresVerifiedEmail pins the ownership check: an
// allowlist entry matching the email claim only authorizes when the IdP asserts
// email_verified; subject entries stay independent of the email claim.
func TestRPCallbackAllowlistRequiresVerifiedEmail(t *testing.T) {
	t.Run("email allowlist rejects unverified email", func(t *testing.T) {
		idp := newMockIdP(t)
		_, err := issueResult(t, idp, []string{"user@example.com"})
		if err == nil || !strings.Contains(err.Error(), "not in the allowlist") {
			t.Fatalf("unverified email must not satisfy the email allowlist, got %v", err)
		}
	})

	t.Run("email allowlist accepts verified email string claim", func(t *testing.T) {
		idp := newMockIdP(t)
		idp.emailVerified = "true" // some providers emit strings
		result, err := issueResult(t, idp, []string{"user@example.com"})
		if err != nil {
			t.Fatalf("verified email must satisfy the email allowlist: %v", err)
		}
		if result.Identity.Subject != "user-123" {
			t.Fatalf("subject = %q, want user-123", result.Identity.Subject)
		}
	})

	t.Run("subject allowlist ignores unverified email", func(t *testing.T) {
		idp := newMockIdP(t)
		result, err := issueResult(t, idp, []string{"user-123"})
		if err != nil {
			t.Fatalf("subject allowlist must not depend on email claims: %v", err)
		}
		if result.Identity.EmailVerified {
			t.Fatal("unverified email must not be reported as verified")
		}
	})
}

// TestRPDiscoveryFailureCached pins the bounded retry: a failed discovery is
// served from cache for discoveryRetryInterval instead of hammering the IdP.
func TestRPDiscoveryFailureCached(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "discovery broken", http.StatusInternalServerError)
	}))
	defer server.Close()

	base := time.Now()
	rp := NewRP(Config{
		Issuer:       server.URL,
		ClientID:     "smoke-client",
		ClientSecret: "smoke-secret",
		RedirectURL:  "https://keeper.example.com/api/v1/auth/oidc/callback",
	})
	rp.now = func() time.Time { return base }
	ctx := context.Background()

	if _, err := rp.Login(ctx); err == nil {
		t.Fatal("expected discovery failure")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("discovery hits = %d, want 1", got)
	}

	// Immediate retry must be served from cache without touching the IdP.
	if _, err := rp.Login(ctx); err == nil {
		t.Fatal("expected cached discovery failure")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("cached retry hit the IdP: hits = %d, want 1", got)
	}

	// After the bounded interval the RP retries discovery.
	rp.now = func() time.Time { return base.Add(discoveryRetryInterval + time.Second) }
	if _, err := rp.Login(ctx); err == nil {
		t.Fatal("expected retried discovery failure")
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("retry hits = %d, want 2", got)
	}
}

func TestValidateIssuerURL(t *testing.T) {
	cases := []struct {
		raw      string
		insecure bool
		wantErr  bool
	}{
		{"https://sso.example.com/realms/keeper", false, false},
		{"http://localhost:8080", false, false},
		{"http://127.0.0.1:9999", false, false},
		{"http://sso.example.com", false, true},
		{"/relative/path", false, true},
		{"", false, true},
		// allowInsecure 只放宽 https 要求，仍然要求绝对 http(s) 地址。
		{"http://sso.example.com", true, false},
		{"https://sso.example.com", true, false},
		{"/relative/path", true, true},
		{"ftp://sso.example.com", true, true},
		{"", true, true},
	}
	for _, tc := range cases {
		err := ValidateIssuerURL(tc.raw, tc.insecure)
		if (err != nil) != tc.wantErr {
			t.Fatalf("ValidateIssuerURL(%q, insecure=%v) error = %v, wantErr %v", tc.raw, tc.insecure, err, tc.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "AUTH_OIDC_ISSUER") {
			t.Fatalf("ValidateIssuerURL(%q, insecure=%v) error should name AUTH_OIDC_ISSUER, got %q", tc.raw, tc.insecure, err)
		}
	}
}

// signIDToken signs a compact JWS with RS256 using go-jose.
func signIDToken(key *rsa.PrivateKey, header, payload map[string]any) (string, error) {
	extraHeaders := make(map[jose.HeaderKey]interface{}, len(header))
	for k, v := range header {
		extraHeaders[jose.HeaderKey(k)] = v
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, &jose.SignerOptions{ExtraHeaders: extraHeaders})
	if err != nil {
		return "", err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	object, err := signer.Sign(rawPayload)
	if err != nil {
		return "", err
	}
	return object.CompactSerialize()
}
