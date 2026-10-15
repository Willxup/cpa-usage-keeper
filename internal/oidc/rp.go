package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	// stateTTL is how long a pending browser redirect stays valid.
	stateTTL = 5 * time.Minute
	// stateMaxEntries bounds memory usage for abandoned logins.
	stateMaxEntries = 1024
	// discoveryRetryInterval bounds how long a failed discovery is cached before retrying,
	// so an outage or misconfiguration does not turn every login into an IdP request.
	discoveryRetryInterval = 30 * time.Second
)

// Config holds the validated OIDC relying-party settings.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AllowedUsers []string
}

// Identity is the verified end-user identity extracted from the ID token.
type Identity struct {
	Subject string
	Email   string
	// EmailVerified reports whether the IdP asserts ownership of the email claim;
	// allowlist entries that match by email require it.
	EmailVerified bool
	Name          string
}

// LoginResult carries the redirect target for starting the browser flow.
type LoginResult struct {
	AuthorizationURL string
}

// CallbackResult carries the verified identity after the IdP redirected back.
type CallbackResult struct {
	Identity Identity
}

// provider holds the lazily discovered and cached OIDC provider state.
type provider struct {
	oidc     *oidc.Provider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// RP implements the OIDC relying-party authorization-code flow with PKCE.
type RP struct {
	config Config

	mu       sync.Mutex
	provider *provider
	// providerErr caches the last discovery failure to avoid hammering a broken IdP.
	providerErr error
	providerAt  time.Time

	states   map[string]*pendingState
	statesMu sync.Mutex
	now      func() time.Time
}

// pendingState tracks one in-flight browser login.
type pendingState struct {
	verifier string // PKCE code_verifier
	created  time.Time
}

// NewRP builds a relying-party client. Discovery happens lazily on first use.
func NewRP(config Config) *RP {
	return &RP{
		config: config,
		states: make(map[string]*pendingState),
		now:    time.Now,
	}
}

// refreshProvider returns the cached provider, refreshing it after failures or long idle.
func (r *RP) refreshProvider(ctx context.Context) (*provider, error) {
	if r == nil {
		return nil, errors.New("oidc rp is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.provider != nil && r.providerErr == nil && r.now().Sub(r.providerAt) < time.Hour {
		return r.provider, nil
	}
	// Serve the cached failure for a bounded interval so outages do not hammer the IdP.
	if r.providerErr != nil && r.now().Sub(r.providerAt) < discoveryRetryInterval {
		return nil, r.providerErr
	}
	oidcProvider, err := oidc.NewProvider(ctx, r.config.Issuer)
	if err != nil {
		r.provider = nil
		r.providerErr = err
		r.providerAt = r.now()
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	r.provider = &provider{
		oidc: oidcProvider,
		oauth: oauth2.Config{
			ClientID:     r.config.ClientID,
			ClientSecret: r.config.ClientSecret,
			Endpoint:     oidcProvider.Endpoint(),
			RedirectURL:  r.config.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: oidcProvider.Verifier(&oidc.Config{ClientID: r.config.ClientID}),
	}
	r.providerErr = nil
	r.providerAt = r.now()
	return r.provider, nil
}

// Login starts the authorization-code flow: returns the IdP redirect URL.
func (r *RP) Login(ctx context.Context) (*LoginResult, error) {
	provider, err := r.refreshProvider(ctx)
	if err != nil {
		return nil, err
	}
	state, verifier, err := newStateWithVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate oidc state: %w", err)
	}
	r.storeState(state, &pendingState{verifier: verifier, created: r.now()})

	authURL := provider.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	return &LoginResult{AuthorizationURL: authURL}, nil
}

// Callback finishes the flow: exchanges the code and verifies the ID token.
func (r *RP) Callback(ctx context.Context, state, code string) (*CallbackResult, error) {
	verifier, ok := r.takeState(state)
	if !ok {
		return nil, errors.New("oidc state is invalid or expired")
	}
	provider, err := r.refreshProvider(ctx)
	if err != nil {
		return nil, err
	}
	token, err := provider.oauth.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return nil, fmt.Errorf("exchange oidc authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("oidc token response missing id_token")
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify oidc id token: %w", err)
	}
	var claims struct {
		Sub           string          `json:"sub"`
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"`
		Name          string          `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode oidc id token claims: %w", err)
	}
	if strings.TrimSpace(claims.Sub) == "" {
		return nil, errors.New("oidc id token missing subject claim")
	}
	identity := Identity{
		Subject:       claims.Sub,
		Email:         strings.TrimSpace(claims.Email),
		EmailVerified: parseEmailVerified(claims.EmailVerified),
		Name:          strings.TrimSpace(claims.Name),
	}
	if allowed := r.config.AllowedUsers; len(allowed) > 0 && !identityAllowed(identity, allowed) {
		return nil, errors.New("oidc identity is not in the allowlist")
	}
	return &CallbackResult{Identity: identity}, nil
}

// identityAllowed matches subject or email (case-insensitive) against the allowlist.
// Email entries only match when the IdP asserts email_verified, so an attacker
// controlling the email claim cannot claim an allowlisted address.
func identityAllowed(identity Identity, allowed []string) bool {
	normalized := make([]string, 0, len(allowed))
	for _, entry := range allowed {
		if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" {
			normalized = append(normalized, entry)
		}
	}
	subject := strings.ToLower(identity.Subject)
	email := strings.ToLower(identity.Email)
	for _, entry := range normalized {
		if subject != "" && entry == subject {
			return true
		}
		if identity.EmailVerified && email != "" && entry == email {
			return true
		}
	}
	return false
}

// parseEmailVerified accepts the standard boolean form plus the string form
// ("true"/"1") some providers emit; missing or malformed claims mean unverified.
func parseEmailVerified(raw json.RawMessage) bool {
	trimmed := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch trimmed {
	case "true", "1":
		return true
	default:
		return false
	}
}

// storeState remembers a pending login with TTL and bounded capacity.
func (r *RP) storeState(key string, state *pendingState) {
	r.statesMu.Lock()
	defer r.statesMu.Unlock()
	r.expireStatesLocked()
	if len(r.states) >= stateMaxEntries {
		// Drop the oldest entry; login redirects are naturally rate-limited anyway.
		var oldestKey string
		var oldest time.Time
		for key, entry := range r.states {
			if oldestKey == "" || entry.created.Before(oldest) {
				oldestKey, oldest = key, entry.created
			}
		}
		if oldestKey != "" {
			delete(r.states, oldestKey)
		}
	}
	r.states[key] = state
}

// takeState removes and returns the pending verifier for a state, if valid.
func (r *RP) takeState(key string) (string, bool) {
	if strings.TrimSpace(key) == "" {
		return "", false
	}
	r.statesMu.Lock()
	defer r.statesMu.Unlock()
	r.expireStatesLocked()
	state, ok := r.states[key]
	if !ok {
		return "", false
	}
	delete(r.states, key)
	if r.now().Sub(state.created) > stateTTL {
		return "", false
	}
	return state.verifier, true
}

func (r *RP) expireStatesLocked() {
	now := r.now()
	for key, state := range r.states {
		if now.Sub(state.created) > stateTTL {
			delete(r.states, key)
		}
	}
}

// newStateWithVerifier generates the anti-CSRF state and the PKCE verifier.
func newStateWithVerifier() (state, verifier string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	state = base64.RawURLEncoding.EncodeToString(buf)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	return state, verifier, nil
}

// codeChallenge derives the S256 code challenge from the verifier.
func codeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// ValidateIssuerURL checks that an OIDC issuer URL is absolute and https or loopback http,
// so discovery and JWKS cannot be intercepted on a plaintext non-loopback hop.
// allowInsecure relaxes the https requirement for plain-http IdPs on trusted networks,
// while still requiring an absolute http(s) URL.
func ValidateIssuerURL(raw string, allowInsecure bool) error {
	if allowInsecure {
		parsed, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("AUTH_OIDC_ISSUER is invalid: %w", err)
		}
		if !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("AUTH_OIDC_ISSUER must be an absolute http(s) URL")
		}
		return nil
	}
	if err := validateHTTPSURL(raw); err != nil {
		return fmt.Errorf("AUTH_OIDC_ISSUER %w", err)
	}
	return nil
}

// ValidateRedirectURL checks that a configured redirect URL is absolute and https or loopback http.
func ValidateRedirectURL(raw string) error {
	if err := validateHTTPSURL(raw); err != nil {
		return fmt.Errorf("AUTH_OIDC_REDIRECT_URL %w", err)
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("is invalid: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return errors.New("must be an absolute URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
	}
	return errors.New("must use https (http allowed only for loopback)")
}
