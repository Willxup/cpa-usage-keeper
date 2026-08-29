package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	keeperauth "cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestPasswordLoginCannotBypassAttemptLimitWithCorrectPassword(t *testing.T) {
	router := newLoginSecurityRouter(nil)
	remoteAddr := "198.51.100.21:1234"
	for index := 0; index < 5; index++ {
		response := performLoginRequest(router, "/api/v1/auth/login", `{"password":"wrong"}`, remoteAddr)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", index+1, response.Code)
		}
	}

	response := performLoginRequest(router, "/api/v1/auth/login", `{"password":"secret"}`, remoteAddr)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected a correct password to remain rate limited, got %d", response.Code)
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter == "" {
		t.Fatal("expected rate-limited login to include Retry-After")
	}
}

func TestAPIKeyLoginDoesNotQueryProviderWhenAttemptLimitIsReached(t *testing.T) {
	provider := &countingCPAAPIKeyProvider{findErr: errors.New("not found")}
	router := newLoginSecurityRouter(provider)
	remoteAddr := "198.51.100.22:1234"
	for index := 0; index < 5; index++ {
		response := performLoginRequest(router, "/api/v1/auth/api-key-login", `{"apiKey":"missing"}`, remoteAddr)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", index+1, response.Code)
		}
	}

	response := performLoginRequest(router, "/api/v1/auth/api-key-login", `{"apiKey":"still-missing"}`, remoteAddr)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected API key login to be rate limited, got %d", response.Code)
	}
	if provider.findCalls != 5 {
		t.Fatalf("expected the provider not to be queried after limiting, got %d calls", provider.findCalls)
	}
}

func TestUnauthenticatedLoginEndpointsRejectBodiesLargerThanFourKiB(t *testing.T) {
	router := newLoginSecurityRouter(&countingCPAAPIKeyProvider{findErr: errors.New("not found")})
	for _, testCase := range []struct {
		name string
		path string
		body string
	}{
		{name: "password", path: "/api/v1/auth/login", body: `{"password":"` + strings.Repeat("x", 4096) + `"}`},
		{name: "api key", path: "/api/v1/auth/api-key-login", body: `{"apiKey":"` + strings.Repeat("x", 4096) + `"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performLoginRequest(router, testCase.path, testCase.body, "198.51.100.23:1234")
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413, got %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUnauthenticatedLoginEndpointsRejectChunkedBodiesLargerThanFourKiB(t *testing.T) {
	router := newLoginSecurityRouter(&countingCPAAPIKeyProvider{findErr: errors.New("not found")})
	for _, testCase := range []struct {
		name string
		path string
		body string
	}{
		{name: "password", path: "/api/v1/auth/login", body: `{"password":"` + strings.Repeat("x", 4096) + `"}`},
		{name: "api key", path: "/api/v1/auth/api-key-login", body: `{"apiKey":"` + strings.Repeat("x", 4096) + `"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := newLoginRequest(testCase.path, testCase.body, "198.51.100.24:1234")
			request.ContentLength = -1
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413, got %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUnauthenticatedLoginLimitsRunBeforeRequestIntentCheck(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		basePath string
		path     string
	}{
		{name: "password login", path: "/api/v1/auth/login"},
		{name: "API key login with base path", basePath: "/cpa", path: "/cpa/api/v1/auth/api-key-login"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := newLoginSecurityRouterWithBasePath(nil, testCase.basePath)
			writer := newDeadlineTrackingResponseWriter()
			body := &deadlineCheckingBody{
				reader:         strings.NewReader("x"),
				deadlineActive: writer.readDeadlineActive,
			}
			request := httptest.NewRequest(http.MethodPost, testCase.path, nil)
			request.Body = body
			request.ContentLength = 1
			request.Header.Set("Content-Type", "application/json")
			request.RemoteAddr = "198.51.100.25:1234"

			router.ServeHTTP(writer, request)

			if writer.Code != http.StatusForbidden {
				t.Fatalf("expected missing request intent to return 403, got %d", writer.Code)
			}
			if !writer.sawReadDeadline {
				t.Fatal("expected login read deadline to be set before request intent validation")
			}
			if !body.closedWithDeadline {
				t.Fatal("expected rejected login body to close while the read deadline is active")
			}
		})
	}
}

func TestKnownOversizedLoginBodyClosesAfterReadDeadline(t *testing.T) {
	router := newLoginSecurityRouter(nil)
	writer := newDeadlineTrackingResponseWriter()
	body := &deadlineCheckingBody{
		reader:         strings.NewReader("x"),
		deadlineActive: writer.readDeadlineActive,
	}
	request := newLoginRequest("/api/v1/auth/login", "", "198.51.100.26:1234")
	request.Body = body
	request.ContentLength = 4097

	router.ServeHTTP(writer, request)

	if writer.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized login body to return 413, got %d", writer.Code)
	}
	if !body.closedWithDeadline {
		t.Fatal("expected oversized login body to close while the read deadline is active")
	}
}

func TestTrustedLoopbackProxySeparatesLoginAttemptSources(t *testing.T) {
	router := newLoginSecurityRouter(nil)
	for index := 0; index < 5; index++ {
		response := performForwardedLoginRequest(router, "198.51.100.31", "127.0.0.1:4100")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", index+1, response.Code)
		}
	}

	otherClient := performForwardedLoginRequest(router, "198.51.100.32", "127.0.0.1:4101")
	if otherClient.Code != http.StatusUnauthorized {
		t.Fatalf("expected a different client behind the trusted proxy to keep its own budget, got %d", otherClient.Code)
	}

	limitedClient := performForwardedLoginRequest(router, "198.51.100.31", "127.0.0.1:4102")
	if limitedClient.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the original forwarded client to remain limited, got %d", limitedClient.Code)
	}
}

func TestDirectClientCannotSpoofLoginSourceWithForwardedHeader(t *testing.T) {
	router := newLoginSecurityRouter(nil)
	for index := 0; index < 5; index++ {
		response := performForwardedLoginRequest(router, "203.0.113."+strconv.Itoa(index+1), "198.51.100.41:4200")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", index+1, response.Code)
		}
	}

	response := performForwardedLoginRequest(router, "203.0.113.99", "198.51.100.41:4201")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected direct client to remain limited despite changing X-Forwarded-For, got %d", response.Code)
	}
}

func TestExplicitTrustedProxyCIDRSeparatesLoginAttemptSources(t *testing.T) {
	config := keeperapi.AuthConfig{
		Enabled:           true,
		LoginPassword:     "secret",
		SessionTTL:        time.Hour,
		TrustedProxyCIDRs: []string{"192.0.2.0/24"},
	}
	sessions := keeperauth.NewSessionManager(time.Hour)
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "")

	for index := 0; index < 5; index++ {
		response := performForwardedLoginRequest(router, "198.51.100.51", "192.0.2.10:4300")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d: expected 401, got %d", index+1, response.Code)
		}
	}
	response := performForwardedLoginRequest(router, "198.51.100.52", "192.0.2.10:4301")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected configured proxy CIDR to separate forwarded clients, got %d", response.Code)
	}
}

func TestAuthenticatedBusinessRouteDoesNotUseLoginBodyLimit(t *testing.T) {
	sessions := keeperauth.NewSessionManager(time.Hour)
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := keeperapi.NewAuthHandler(config, sessions)
	provider := &authFilesManagementProvider{}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, handler, "", keeperapi.OptionalProviders{AuthFiles: provider})
	token, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/auth-files/status", strings.NewReader(`{"names":["`+strings.Repeat("x", 4096)+`"],"disabled":true}`))
	request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected authenticated business body to remain accepted, got %d body=%s", response.Code, response.Body.String())
	}
}

func newLoginSecurityRouter(provider service.CPAAPIKeyProvider) http.Handler {
	return newLoginSecurityRouterWithBasePath(provider, "")
}

func newLoginSecurityRouterWithBasePath(provider service.CPAAPIKeyProvider, basePath string) http.Handler {
	sessions := keeperauth.NewSessionManager(time.Hour)
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: basePath}
	handler := keeperapi.NewAuthHandler(config, sessions)
	return keeperapi.NewRouter(nil, nil, nil, nil, config, handler, basePath, keeperapi.OptionalProviders{CPAAPIKeys: provider})
}

func performLoginRequest(router http.Handler, path, body, remoteAddr string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := newLoginRequest(path, body, remoteAddr)
	router.ServeHTTP(response, request)
	return response
}

func performForwardedLoginRequest(router http.Handler, clientIP, remoteAddr string) *httptest.ResponseRecorder {
	request := newLoginRequest("/api/v1/auth/login", `{"password":"wrong"}`, remoteAddr)
	request.Header.Set("X-Forwarded-For", clientIP)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func newLoginRequest(path, body, remoteAddr string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	return request
}

type countingCPAAPIKeyProvider struct {
	findCalls int
	findErr   error
}

func (p *countingCPAAPIKeyProvider) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return nil, nil
}

func (p *countingCPAAPIKeyProvider) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	p.findCalls++
	return entities.CPAAPIKey{}, p.findErr
}

func (p *countingCPAAPIKeyProvider) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, errors.New("not found")
}

func (p *countingCPAAPIKeyProvider) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, nil
}

type authFilesManagementProvider struct{}

func (p *authFilesManagementProvider) SetAuthFilesDisabled(_ context.Context, names []string, _ bool) (service.AuthFilesManagementResponse, error) {
	return service.AuthFilesManagementResponse{Names: names, Affected: len(names)}, nil
}

func (p *authFilesManagementProvider) DeleteAuthFiles(_ context.Context, names []string) (service.AuthFilesManagementResponse, error) {
	return service.AuthFilesManagementResponse{Names: names, Affected: len(names)}, nil
}

type deadlineTrackingResponseWriter struct {
	*httptest.ResponseRecorder
	readDeadline    time.Time
	sawReadDeadline bool
}

func newDeadlineTrackingResponseWriter() *deadlineTrackingResponseWriter {
	return &deadlineTrackingResponseWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (w *deadlineTrackingResponseWriter) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	if !deadline.IsZero() {
		w.sawReadDeadline = true
	}
	return nil
}

func (w *deadlineTrackingResponseWriter) readDeadlineActive() bool {
	return !w.readDeadline.IsZero()
}

type deadlineCheckingBody struct {
	reader             io.Reader
	deadlineActive     func() bool
	closedWithDeadline bool
}

func (b *deadlineCheckingBody) Read(buffer []byte) (int, error) {
	return b.reader.Read(buffer)
}

func (b *deadlineCheckingBody) Close() error {
	b.closedWithDeadline = b.deadlineActive != nil && b.deadlineActive()
	return nil
}
