package test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/oidc"
)

type mockIDP struct {
	server                              *httptest.Server
	key                                 *rsa.PrivateKey
	issuer                              string
	style                               string
	claims                              func(map[string]any)
	metadata                            func(map[string]any)
	userinfo                            map[string]any
	failDiscovery                       bool
	cacheControl                        string
	queryRequests                       atomic.Int64
	mu                                  sync.Mutex
	codes                               map[string]url.Values
	discoveries, keyRequests, exchanges atomic.Int64
}

func newMockIDP(t *testing.T, tls bool, issuerPath string) *mockIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m := &mockIDP{key: key, codes: make(map[string]url.Values), style: "basic"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.serveHTTP(w, r) })
	if tls {
		m.server = httptest.NewTLSServer(handler)
	} else {
		m.server = httptest.NewServer(handler)
	}
	m.issuer = m.server.URL + issuerPath
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockIDP) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration"):
		m.discoveries.Add(1)
		if m.failDiscovery {
			http.Error(w, "secret upstream error", 503)
			return
		}
		w.Header().Set("Cache-Control", m.cacheControl)
		metadata := map[string]any{"issuer": m.issuer, "authorization_endpoint": m.server.URL + "/authorize", "token_endpoint": m.server.URL + "/token", "jwks_uri": m.server.URL + "/keys", "response_types_supported": []string{"code"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"}}
		if m.userinfo != nil {
			metadata["userinfo_endpoint"] = m.server.URL + "/userinfo"
		}
		if m.metadata != nil {
			m.metadata(metadata)
		}
		_ = json.NewEncoder(w).Encode(metadata)
	case r.URL.Path == "/keys":
		if r.URL.Query().Get("existing") == "keys" {
			m.queryRequests.Add(1)
		}
		m.keyRequests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "key-1", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(m.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.key.E)).Bytes())}}})
	case r.URL.Path == "/token":
		if r.URL.Query().Get("existing") == "token" {
			m.queryRequests.Add(1)
		}
		m.exchanges.Add(1)
		_ = r.ParseForm()
		client, secret, basic := r.BasicAuth()
		if m.style == "post" {
			if basic || r.PostForm.Get("client_id") != "keeper" || r.PostForm.Get("client_secret") != "secret" {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
		} else if !basic || client != "keeper" || secret != "secret" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		m.mu.Lock()
		authorization := m.codes[r.PostForm.Get("code")]
		delete(m.codes, r.PostForm.Get("code"))
		m.mu.Unlock()
		challenge := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if authorization == nil || authorization.Get("code_challenge_method") != "S256" || authorization.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || r.PostForm.Get("redirect_uri") != authorization.Get("redirect_uri") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		claims := map[string]any{"iss": m.issuer, "sub": "Subject,A", "aud": "keeper", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": authorization.Get("nonce"), "name": "Keeper Admin"}
		if m.claims != nil {
			m.claims(claims)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": signedJWT(m.key, claims)})
	case r.URL.Path == "/userinfo":
		if r.URL.Query().Get("existing") == "userinfo" {
			m.queryRequests.Add(1)
		}
		if r.Header.Get("Authorization") != "Bearer access" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(m.userinfo)
	default:
		http.NotFound(w, r)
	}
}

func signedJWT(key *rsa.PrivateKey, claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key-1"}`))
	payload, _ := json.Marshal(claims)
	input := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (m *mockIDP) client(t *testing.T, subjects []string) *oidc.Client {
	t.Helper()
	client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback", AllowedSubjects: subjects}, m.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func (m *mockIDP) code(t *testing.T, authorization oidc.Authorization) string {
	t.Helper()
	u, err := url.Parse(authorization.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	if query.Get("response_type") != "code" || query.Get("scope") != "openid profile email" || query.Get("response_mode") != "query" || query.Get("nonce") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("invalid authorization request: %v", query)
	}
	code := fmt.Sprintf("code-%s", authorization.State)
	m.mu.Lock()
	m.codes[code] = query
	m.mu.Unlock()
	return code
}

func loginMock(t *testing.T, m *mockIDP, client *oidc.Client) (oidc.LoginResult, error) {
	t.Helper()
	authorization, err := client.Begin(context.Background(), "/auth-files?credential=123#detail", nil, "")
	if err != nil {
		return oidc.LoginResult{}, err
	}
	return client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), "")
}

func TestOIDCCodeFlowHTTPHTTPSPathAndClientAuth(t *testing.T) {
	for _, tls := range []bool{false, true} {
		for _, style := range []string{"basic", "post"} {
			t.Run(fmt.Sprintf("tls=%v/%s", tls, style), func(t *testing.T) {
				m := newMockIDP(t, tls, "/tenant/realm/")
				m.style = style
				client := m.client(t, []string{"Subject,A"})
				result, err := loginMock(t, m, client)
				if err != nil || result.Identity.Subject != "Subject,A" || result.Identity.Issuer != m.issuer || result.ReturnTo != "/auth-files?credential=123#detail" {
					t.Fatalf("login = %+v, %v", result, err)
				}
				if _, err := loginMock(t, m, client); err != nil {
					t.Fatal(err)
				}
				if m.discoveries.Load() != 1 || m.keyRequests.Load() != 1 {
					t.Fatalf("cache not reused: Discovery=%d JWKS=%d", m.discoveries.Load(), m.keyRequests.Load())
				}
				wantExchanges := int64(2)
				if style == "post" {
					wantExchanges++
				}
				if m.exchanges.Load() != wantExchanges {
					t.Fatalf("authentication detection not cached: exchanges=%d", m.exchanges.Load())
				}
			})
		}
	}
}

func TestOIDCPromptLoginAuthorization(t *testing.T) {
	m := newMockIDP(t, false, "")
	m.metadata = func(metadata map[string]any) {
		metadata["authorization_endpoint"] = m.server.URL + "/authorize?tenant=existing&prompt=consent"
	}
	for _, promptLogin := range []bool{true, false} {
		t.Run(fmt.Sprint(promptLogin), func(t *testing.T) {
			client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback", PromptLogin: promptLogin}, m.server.Client())
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := client.Begin(context.Background(), "/", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(authorization.URL)
			values, present := u.Query()["prompt"]
			if promptLogin && (len(values) != 1 || values[0] != "login") || !promptLogin && present {
				t.Fatalf("prompt parameter = %v, enabled=%v", values, promptLogin)
			}
			if u.Query().Get("tenant") != "existing" {
				t.Fatal("existing authorization endpoint query was lost")
			}
			result, err := client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), "")
			if err != nil || result.Identity.Subject != "Subject,A" {
				t.Fatalf("login = %+v, %v", result, err)
			}
		})
	}
}

func TestOIDCRejectsInvalidSignedClaimsAndSubject(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"issuer":         func(c map[string]any) { c["iss"] = "https://other.example" },
		"empty subject":  func(c map[string]any) { c["sub"] = "" },
		"subject type":   func(c map[string]any) { c["sub"] = 123 },
		"subject case":   func(c map[string]any) { c["sub"] = "subject,A" },
		"nonce":          func(c map[string]any) { c["nonce"] = "wrong" },
		"missing nonce":  func(c map[string]any) { delete(c, "nonce") },
		"audience":       func(c map[string]any) { c["aud"] = "other" },
		"extra audience": func(c map[string]any) { c["aud"] = []string{"keeper", "other"}; c["azp"] = "keeper" },
		"azp":            func(c map[string]any) { c["azp"] = "other" },
		"expired":        func(c map[string]any) { c["exp"] = time.Now().Add(-2 * time.Minute).Unix() },
		"exp string":     func(c map[string]any) { c["exp"] = "9999999999" },
		"missing exp":    func(c map[string]any) { delete(c, "exp") },
		"missing iat":    func(c map[string]any) { delete(c, "iat") },
		"iat string":     func(c map[string]any) { c["iat"] = "1234" },
		"iat null":       func(c map[string]any) { c["iat"] = nil },
		"future iat":     func(c map[string]any) { c["iat"] = time.Now().Add(2 * time.Minute).Unix() },
		"future nbf":     func(c map[string]any) { c["nbf"] = time.Now().Add(2 * time.Minute).Unix() },
		"at_hash":        func(c map[string]any) { c["at_hash"] = "wrong" },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMockIDP(t, false, "")
			m.claims = change
			result, err := loginMock(t, m, m.client(t, []string{"Subject,A"}))
			if err == nil {
				t.Fatal("invalid identity accepted")
			}
			if !result.AuthenticationFailed {
				t.Fatal("invalid identity was not classified as authentication failure")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "code-") {
				t.Fatalf("sensitive error: %v", err)
			}
		})
	}
}

func TestOIDCOptionalClaimsAndUserInfoSubject(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			m := newMockIDP(t, false, "")
			m.claims = func(c map[string]any) {
				delete(c, "name")
				c["aud"] = []string{"keeper"}
				c["unknown"] = []string{"ignored"}
				c["email_verified"] = "not-used"
				hash := sha256.Sum256([]byte("access"))
				c["at_hash"] = base64.RawURLEncoding.EncodeToString(hash[:16])
			}
			m.userinfo = map[string]any{"sub": "Subject,A", "name": "UserInfo name"}
			if mismatch {
				m.userinfo["sub"] = "different"
			}
			result, err := loginMock(t, m, m.client(t, nil))
			if mismatch && err == nil {
				t.Fatal("userinfo changed verified subject")
			}
			if !mismatch && (err != nil || result.Identity.Name != "UserInfo name") {
				t.Fatalf("login = %+v, %v", result, err)
			}
		})
	}
	m := newMockIDP(t, false, "")
	m.claims = func(c map[string]any) { delete(c, "name"); delete(c, "email") }
	result, err := loginMock(t, m, m.client(t, nil))
	if err != nil || result.Identity.Subject == "" {
		t.Fatalf("subject-only login failed: %+v %v", result, err)
	}
}

func TestOIDCBrowserBindingSingleConsumptionAndPKCE(t *testing.T) {
	m := newMockIDP(t, false, "")
	client := m.client(t, nil)
	authorization, err := client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	code := m.code(t, authorization)
	if _, err := client.Complete(context.Background(), authorization.State, "another-browser", code, ""); !errors.Is(err, oidc.ErrInvalid) {
		t.Fatalf("wrong browser = %v", err)
	}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, code, ""); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent callback successes = %d", successes.Load())
	}
	authorization, _ = client.Begin(context.Background(), "/", nil, "")
	code = m.code(t, authorization)
	m.mu.Lock()
	m.codes[code].Set("code_challenge", "wrong-verifier")
	m.mu.Unlock()
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, code, ""); err == nil {
		t.Fatal("wrong PKCE verifier accepted")
	}
}

func TestOIDCMetadataFailureBackoffAndCanceledWaiter(t *testing.T) {
	hook := captureOIDCLogs(t)
	m := newMockIDP(t, false, "")
	m.failDiscovery = true
	client := m.client(t, nil)
	for range 3 {
		if _, err := client.Begin(context.Background(), "/", nil, ""); !errors.Is(err, oidc.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	requireOIDCDiagnostic(t, hook, "discovery", "unexpected_http_status", 503, "", "secret upstream error")
	if m.discoveries.Load() != 1 || len(hook.AllEntries()) != 1 {
		t.Fatal("cached discovery failure produced repeated requests or diagnostics")
	}
	m = newMockIDP(t, false, "")
	client = m.client(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = client.Begin(ctx, "/", nil, "")
	if _, err := loginMock(t, m, client); err != nil {
		t.Fatalf("canceled caller poisoned cache: %v", err)
	}
}

func TestOIDCMetadataRejectsExplicitUnsupportedCapabilities(t *testing.T) {
	for field, value := range map[string]any{"issuer": "https://other.example", "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"plain"}, "id_token_signing_alg_values_supported": []string{"HS256", "none"}, "response_types_supported": []string{"id_token"}, "response_modes_supported": []string{"form_post"}, "grant_types_supported": []string{"client_credentials"}, "jwks_uri": "file:///keys"} {
		t.Run(field, func(t *testing.T) {
			hook := captureOIDCLogs(t)
			m := newMockIDP(t, false, "")
			m.metadata = func(c map[string]any) { c[field] = value }
			if _, err := m.client(t, nil).Begin(context.Background(), "/", nil, ""); !errors.Is(err, oidc.ErrUnavailable) {
				t.Fatalf("unsupported metadata = %v", err)
			}
			if field == "issuer" {
				requireOIDCDiagnostic(t, hook, "discovery", "issuer_mismatch", 0, "", "other.example", "secret")
			} else if field == "response_types_supported" {
				requireOIDCDiagnostic(t, hook, "discovery", "unsupported_authorization_capabilities", 0, "", "secret")
			}
		})
	}
}

func TestOIDCTransportRejectsUntrustedTLSAndCredentialRedirect(t *testing.T) {
	hook := captureOIDCLogs(t)
	m := newMockIDP(t, true, "")
	client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Begin(context.Background(), "/", nil, ""); !errors.Is(err, oidc.ErrUnavailable) {
		t.Fatalf("untrusted TLS = %v", err)
	}
	requireOIDCDiagnostic(t, hook, "discovery", "tls_certificate_invalid", 0, "", "secret")
	var leaked atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true); w.WriteHeader(500) }))
	defer receiver.Close()
	m = newMockIDP(t, false, "")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, receiver.URL, 307) }))
	defer redirector.Close()
	m.metadata = func(c map[string]any) { c["token_endpoint"] = redirector.URL }
	if _, err := loginMock(t, m, m.client(t, nil)); err == nil {
		t.Fatal("redirected token endpoint accepted")
	}
	if leaked.Load() {
		t.Fatal("credentials followed token endpoint redirect")
	}
}

func TestOIDCMetadataEndpointQueriesAndMixedTransports(t *testing.T) {
	m := newMockIDP(t, true, "/realm")
	// 标准端点可分布在不同主机/协议，已有查询参数必须抵达相应端点。
	plain := httptest.NewServer(http.HandlerFunc(m.serveHTTP))
	defer plain.Close()
	m.claims = func(c map[string]any) { delete(c, "name") }
	m.userinfo = map[string]any{"sub": "Subject,A", "name": "UserInfo"}
	m.metadata = func(c map[string]any) {
		c["authorization_endpoint"] = plain.URL + "/authorize?existing=authorization"
		c["token_endpoint"] = plain.URL + "/token?existing=token"
		c["jwks_uri"] = m.server.URL + "/keys?existing=keys"
		c["userinfo_endpoint"] = plain.URL + "/userinfo?existing=userinfo"
	}
	client := m.client(t, nil)
	authorization, err := client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authorization.URL)
	if u.Query().Get("existing") != "authorization" {
		t.Fatal("authorization query dropped")
	}
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), ""); err != nil {
		t.Fatal(err)
	}
	if m.queryRequests.Load() != 3 {
		t.Fatalf("endpoint queries dropped: %d", m.queryRequests.Load())
	}
}

func TestOIDCRejectsWrongSignatureAndUnsignedTokenAndAcceptsRotatedKey(t *testing.T) {
	m := newMockIDP(t, false, "")
	client := m.client(t, nil)
	if _, err := loginMock(t, m, client); err != nil {
		t.Fatal(err)
	}
	rotated, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m.key = rotated
	if _, err := loginMock(t, m, client); err != nil {
		t.Fatalf("rotated key failed: %v", err)
	}
	if m.keyRequests.Load() != 2 {
		t.Fatalf("rotation did not refresh cached JWKS: %d", m.keyRequests.Load())
	}
	for _, algorithm := range []string{"none", "HS256", "RS256"} {
		t.Run(algorithm, func(t *testing.T) {
			bad := newMockIDP(t, false, "")
			wrong, _ := rsa.GenerateKey(rand.Reader, 2048)
			bad.metadata = func(meta map[string]any) {
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = r.ParseForm()
					bad.mu.Lock()
					query := bad.codes[r.Form.Get("code")]
					bad.mu.Unlock()
					claims := map[string]any{"iss": bad.issuer, "sub": "Subject,A", "aud": "keeper", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": query.Get("nonce")}
					jwt := signedJWT(wrong, claims)
					if algorithm != "RS256" {
						parts := strings.Split(jwt, ".")
						parts[0] = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"` + algorithm + `","kid":"key-1"}`))
						jwt = strings.Join(parts, ".")
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": jwt})
				}))
				t.Cleanup(endpoint.Close)
				meta["token_endpoint"] = endpoint.URL
			}
			if result, err := loginMock(t, bad, bad.client(t, nil)); err == nil || !result.AuthenticationFailed {
				t.Fatal("invalid signature accepted")
			}
		})
	}
}

func TestOIDCAcceptsTrustedECDSASigningAlgorithm(t *testing.T) {
	m := newMockIDP(t, false, "")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "EC", "crv": "P-256", "kid": "ec", "x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32)))}}})
			return
		}
		_ = r.ParseForm()
		m.mu.Lock()
		query := m.codes[r.Form.Get("code")]
		m.mu.Unlock()
		claims, _ := json.Marshal(map[string]any{"iss": m.issuer, "sub": "Subject,A", "aud": "keeper", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": query.Get("nonce")})
		input := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","kid":"ec"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims)
		digest := sha256.Sum256([]byte(input))
		left, right, _ := ecdsa.Sign(rand.Reader, key, digest[:])
		signature := append(left.FillBytes(make([]byte, 32)), right.FillBytes(make([]byte, 32))...)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": input + "." + base64.RawURLEncoding.EncodeToString(signature)})
	}))
	defer endpoint.Close()
	m.metadata = func(meta map[string]any) {
		meta["token_endpoint"] = endpoint.URL + "/token"
		meta["jwks_uri"] = endpoint.URL + "/keys"
		meta["id_token_signing_alg_values_supported"] = []string{"ES256", "none", "HS256"}
	}
	if _, err := loginMock(t, m, m.client(t, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCDiscoveryConcurrentRefreshAndExpiredFailure(t *testing.T) {
	m := newMockIDP(t, false, "")
	client := m.client(t, nil)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Begin(context.Background(), "/", nil, ""); err != nil {
				t.Errorf("concurrent start: %v", err)
			}
		}()
	}
	wg.Wait()
	if m.discoveries.Load() != 1 {
		t.Fatalf("concurrent discovery requests = %d", m.discoveries.Load())
	}
	m = newMockIDP(t, false, "")
	m.cacheControl = "no-store"
	client = m.client(t, nil)
	authorization, err := client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	m.failDiscovery = true
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), ""); !errors.Is(err, oidc.ErrUnavailable) {
		t.Fatalf("expired metadata was reused: %v", err)
	}
	if _, err := client.Begin(context.Background(), "/", nil, ""); !errors.Is(err, oidc.ErrUnavailable) {
		t.Fatal(err)
	}
	if m.discoveries.Load() != 2 {
		t.Fatalf("failure backoff did not apply: %d", m.discoveries.Load())
	}
}

func TestOIDCExternalTimeout(t *testing.T) {
	m := newMockIDP(t, false, "")
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer stalled.Close()
	m.metadata = func(meta map[string]any) { meta["token_endpoint"] = stalled.URL }
	httpClient := m.server.Client()
	httpClient.Timeout = 30 * time.Millisecond
	client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback"}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := loginMock(t, m, client); !errors.Is(err, oidc.ErrUnavailable) {
		t.Fatalf("timeout = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("external timeout was not bounded")
	}
}

func TestOIDCClockSkewAcceptsBoundedNumericDates(t *testing.T) {
	m := newMockIDP(t, false, "")
	m.claims = func(claims map[string]any) {
		claims["iat"] = time.Now().Add(-time.Hour).Unix()
		claims["exp"] = time.Now().Add(-30 * time.Second).Unix()
	}
	if _, err := loginMock(t, m, m.client(t, nil)); err != nil {
		t.Fatalf("bounded expiry skew rejected: %v", err)
	}
	m = newMockIDP(t, false, "")
	m.claims = func(claims map[string]any) {
		claims["iat"] = float64(time.Now().Add(30*time.Second).Unix()) + 0.5
		claims["nbf"] = time.Now().Add(30 * time.Second).Unix()
	}
	if _, err := loginMock(t, m, m.client(t, nil)); err != nil {
		t.Fatalf("bounded issued-at/nbf skew rejected: %v", err)
	}
}
