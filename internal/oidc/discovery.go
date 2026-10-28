package oidc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

const (
	ExternalRequestTimeout = 10 * time.Second
	CallbackTimeout        = 30 * time.Second
	discoveryTTL           = time.Hour
	discoveryBackoff       = 30 * time.Second
	maxResponseBytes       = 1 << 20
)

var trustedSigningAlgorithms = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}

type metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	SigningAlgorithms     []string `json:"id_token_signing_alg_values_supported"`
	ResponseTypes         []string `json:"response_types_supported"`
	ResponseModes         []string `json:"response_modes_supported"`
	GrantTypes            []string `json:"grant_types_supported"`
	AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	Scopes                []string `json:"scopes_supported"`
}

type provider struct {
	metadata     metadata
	oauth        *oauth2.Config
	authStyle    *atomic.Int32
	verification coreoidc.Config
	keys         *coreoidc.RemoteKeySet
	expiresAt    time.Time
}

type discoveryFlight struct {
	done     chan struct{}
	provider *provider
	err      error
}

// getProvider 合并刷新但不持锁联网；请求取消只停止自己的等待，不污染长寿命缓存。
func (c *Client) getProvider(ctx context.Context) (*provider, error) {
	c.mu.Lock()
	if c.cached != nil && c.cached.expiresAt.After(time.Now()) {
		p := c.cached
		c.mu.Unlock()
		return p, nil
	}
	flight := c.flight
	if flight == nil {
		if c.retryAt.After(time.Now()) {
			c.mu.Unlock()
			return nil, ErrUnavailable
		}
		flight = &discoveryFlight{done: make(chan struct{})}
		c.flight = flight
		previous := c.cached
		go c.refreshProvider(flight, previous)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ErrUnavailable
	case <-flight.done:
		return flight.provider, flight.err
	}
}

func (c *Client) refreshProvider(flight *discoveryFlight, previous *provider) {
	ctx, cancel := context.WithTimeout(context.Background(), ExternalRequestTimeout)
	defer cancel()
	p, err := c.fetchProvider(ctx, previous)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.retryAt = time.Now().Add(discoveryBackoff)
	} else {
		c.cached = p
		c.retryAt = time.Time{}
	}
	flight.provider, flight.err = p, err
	c.flight = nil
	close(flight.done)
}

func (c *Client) fetchProvider(ctx context.Context, previous *provider) (*provider, error) {
	endpoint := strings.TrimSuffix(c.config.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		logFailure("discovery", "invalid_request")
		return nil, ErrUnavailable
	}
	response, err := c.http.Do(req)
	if err != nil {
		logRequestFailure("discovery", 0, err)
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		logRequestFailure("discovery", response.StatusCode, nil)
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		logFailure("discovery", "response_unreadable_or_too_large")
		return nil, ErrUnavailable
	}
	var m metadata
	if json.Unmarshal(body, &m) != nil {
		logFailure("discovery", "invalid_json")
		return nil, ErrUnavailable
	}
	if m.Issuer != c.config.Issuer {
		logFailure("discovery", "issuer_mismatch")
		return nil, ErrUnavailable
	}
	for _, endpoint := range []string{m.AuthorizationEndpoint, m.TokenEndpoint, m.JWKSURI} {
		if _, err := parseEndpoint(endpoint, true); err != nil {
			logFailure("discovery", "invalid_endpoint")
			return nil, ErrUnavailable
		}
	}
	if m.UserInfoEndpoint != "" {
		if _, err := parseEndpoint(m.UserInfoEndpoint, true); err != nil {
			logFailure("discovery", "invalid_userinfo_endpoint")
			return nil, ErrUnavailable
		}
	}
	// 缺失的可选能力采用标准默认；已声明且不相容的能力明确失败。
	if !slices.Contains(m.ResponseTypes, "code") || !supportsIfPresent(m.ResponseModes, "query") || !supportsIfPresent(m.GrantTypes, "authorization_code") || !supportsIfPresent(m.CodeChallengeMethods, "S256") || !supportsIfPresent(m.Scopes, "openid") {
		logFailure("discovery", "unsupported_authorization_capabilities")
		return nil, ErrUnavailable
	}
	if len(m.AuthMethods) > 0 && !slices.Contains(m.AuthMethods, "client_secret_basic") && !slices.Contains(m.AuthMethods, "client_secret_post") {
		logFailure("discovery", "unsupported_client_authentication")
		return nil, ErrUnavailable
	}
	algorithms := make([]string, 0, len(m.SigningAlgorithms))
	for _, algorithm := range trustedSigningAlgorithms {
		if slices.Contains(m.SigningAlgorithms, algorithm) {
			algorithms = append(algorithms, algorithm)
		}
	}
	if len(algorithms) == 0 {
		logFailure("discovery", "unsupported_signing_algorithms")
		return nil, ErrUnavailable
	}
	var keys *coreoidc.RemoteKeySet
	if previous != nil && previous.metadata.JWKSURI == m.JWKSURI {
		keys = previous.keys
	}
	if keys == nil {
		// 库保存的网络上下文独立于发起登录的请求，HTTP Client 自身仍有超时。
		keys = coreoidc.NewRemoteKeySet(coreoidc.ClientContext(context.Background(), c.http), m.JWKSURI)
	}
	authStyle := &atomic.Int32{}
	if previous != nil && previous.metadata.TokenEndpoint == m.TokenEndpoint {
		authStyle = previous.authStyle
	}
	var oauthConfig *oauth2.Config
	if previous != nil && previous.metadata.TokenEndpoint == m.TokenEndpoint && previous.metadata.AuthorizationEndpoint == m.AuthorizationEndpoint {
		oauthConfig = previous.oauth
	}
	if oauthConfig == nil {
		oauthConfig = &oauth2.Config{ClientID: c.config.ClientID, ClientSecret: c.config.ClientSecret, RedirectURL: c.config.RedirectURL, Scopes: []string{"openid", "profile", "email"}, Endpoint: oauth2.Endpoint{AuthURL: m.AuthorizationEndpoint, TokenURL: m.TokenEndpoint}}
	}
	c.warnHTTPMetadata(m)
	return &provider{metadata: m, oauth: oauthConfig, authStyle: authStyle, keys: keys, verification: coreoidc.Config{ClientID: c.config.ClientID, SupportedSigningAlgs: algorithms, Now: func() time.Time { return time.Now().Add(-ClockSkew) }}, expiresAt: time.Now().Add(cacheLifetime(response.Header))}, nil
}

func supportsIfPresent(values []string, required string) bool {
	return len(values) == 0 || slices.Contains(values, required)
}

func cacheLifetime(header http.Header) time.Duration {
	lifetime := discoveryTTL
	for _, directive := range strings.Split(header.Get("Cache-Control"), ",") {
		parts := strings.SplitN(strings.TrimSpace(directive), "=", 2)
		switch strings.ToLower(parts[0]) {
		case "no-cache", "no-store":
			return 0
		case "max-age":
			if len(parts) == 2 {
				seconds, err := strconv.ParseInt(strings.Trim(parts[1], `"`), 10, 64)
				if err == nil && seconds >= 0 && seconds < int64(lifetime/time.Second) {
					lifetime = time.Duration(seconds) * time.Second
				}
			}
		}
	}
	if expires, err := http.ParseTime(header.Get("Expires")); err == nil && time.Until(expires) < lifetime {
		lifetime = max(0, time.Until(expires))
	}
	if age, err := strconv.ParseInt(header.Get("Age"), 10, 64); err == nil && age > 0 {
		if age >= int64(lifetime/time.Second) {
			return 0
		}
		lifetime -= time.Duration(age) * time.Second
	}
	return lifetime
}

func (c *Client) warnHTTPMetadata(m metadata) {
	for _, endpoint := range []string{m.AuthorizationEndpoint, m.TokenEndpoint, m.JWKSURI, m.UserInfoEndpoint} {
		parsed, err := parseEndpoint(endpoint, true)
		if err == nil && parsed.Scheme == "http" {
			c.httpWarning.Do(func() {
				logrus.Warn("OIDC Discovery declares HTTP endpoints; these connections have no transport encryption")
			})
			return
		}
	}
}
