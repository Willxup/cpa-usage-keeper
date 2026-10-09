package test

import (
	. "cpa-usage-keeper/internal/api"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/version"
	"github.com/gin-gonic/gin"
)

func testStaticFS(t *testing.T, files map[string]string) fs.FS {
	t.Helper()
	staticFS := fstest.MapFS{}
	for name, content := range files {
		staticFS[name] = &fstest.MapFile{Data: []byte(content), Mode: 0o644}
	}
	return staticFS
}

type statusStub struct {
	status poller.Status
}

func (s statusStub) Status() poller.Status {
	return s.status
}

func TestHealthzReturnsOK(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
}

func TestHealthzRemainsPublicWhenAuthEnabled(t *testing.T) {
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, auth.NewSessionManager(time.Hour)), "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestPingOnlyAvailableForDevBuildsOrExplicitDebugMode(t *testing.T) {
	previousVersion := version.Version
	t.Cleanup(func() { version.Version = previousVersion })

	for _, tc := range []struct {
		name, appVersion, ginMode, basePath, path string
		want                                      int
	}{
		{"release root", "v1.2.3", "", "", "/api/v1/ping", http.StatusNotFound},
		{"dev root", "dev", "", "", "/api/v1/ping", http.StatusOK},
		{"debug root", "v1.2.3", gin.DebugMode, "", "/api/v1/ping", http.StatusOK},
		{"release subpath", "v1.2.3", "", "/cpa", "/cpa/api/v1/ping", http.StatusNotFound},
		{"dev subpath", "dev", "", "/cpa", "/cpa/api/v1/ping", http.StatusOK},
		{"debug subpath", "v1.2.3", gin.DebugMode, "/cpa", "/cpa/api/v1/ping", http.StatusOK},
		{"unprefixed subpath", "dev", "", "/cpa", "/api/v1/ping", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version.Version = tc.appVersion
			t.Setenv("GIN_MODE", tc.ginMode)
			router := NewRouter(nil, nil, nil, nil, AuthConfig{BasePath: tc.basePath}, nil, tc.basePath)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if resp.Code != tc.want {
				t.Fatalf("expected status %d, got %d body=%s", tc.want, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestRouterDoesNotTrustForwardedClientIPByDefault(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	router.GET("/client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	req.RemoteAddr = "198.51.100.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Body.String() != "198.51.100.10" {
		t.Fatalf("expected direct remote IP, got %q", resp.Body.String())
	}
}

func TestStatusReturnsPollerState(t *testing.T) {
	router := NewRouter(nil, statusStub{status: poller.Status{
		Running:     true,
		SyncRunning: false,
		LastRunAt:   time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
		LastError:   "boom",
		LastWarning: "metadata unavailable",
		LastStatus:  "completed_with_warnings",
	}}, nil, nil, AuthConfig{}, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"running":true`) && contains(body, `"sync_running":false`) && contains(body, `"last_error":"boom"`) && contains(body, `"last_warning":"metadata unavailable"`) && contains(body, `"last_status":"completed_with_warnings"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if contains(body, `"last_run_at"`) {
		t.Fatalf("status response must not expose poller last run time: %s", body)
	}
}

func TestStatusReturnsProjectTimezone(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"timezone":"Asia/Shanghai"`) {
		t.Fatalf("expected status response to include project timezone, got %s", body)
	}
}

func TestStatusReturnsEmptyStateWithoutProvider(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"running":false`) || !contains(body, `"sync_running":false`) || !contains(body, `"timezone":`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestVersionReturnsCurrentVersionAndUpdateCheckFlag(t *testing.T) {
	previousVersion := version.Version
	t.Cleanup(func() { version.Version = previousVersion })
	version.Version = "v1.2.3"

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if got := resp.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected version Cache-Control no-store, got %q", got)
	}
	if got := resp.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("expected version Pragma no-cache, got %q", got)
	}
	if got := resp.Header().Get("Expires"); got != "0" {
		t.Fatalf("expected version Expires 0, got %q", got)
	}
	var body routerVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Version != "v1.2.3" || !body.UpdateCheckEnabled {
		t.Fatalf("unexpected response body: %+v", body)
	}
}

func TestVersionAuthorizesAdminAndViewerSessionsAtConfiguredBasePath(t *testing.T) {
	for _, basePath := range []string{"", "/cpa"} {
		t.Run("base="+basePath, func(t *testing.T) {
			sessions := auth.NewSessionManager(time.Hour)
			config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: basePath}
			router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), basePath)
			adminToken, _, err := sessions.Create()
			if err != nil {
				t.Fatalf("create admin session: %v", err)
			}
			viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
			if err != nil {
				t.Fatalf("create API key viewer session: %v", err)
			}

			for _, tc := range []struct {
				name, token string
				want        int
			}{
				{"unauthenticated", "", http.StatusUnauthorized},
				{"admin", adminToken, http.StatusOK},
				{"viewer", viewerToken, http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, basePath+"/api/v1/version", nil)
					if tc.token != "" {
						req.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: tc.token})
					}
					resp := httptest.NewRecorder()
					router.ServeHTTP(resp, req)
					if resp.Code != tc.want {
						t.Fatalf("expected status %d, got %d body=%s", tc.want, resp.Code, resp.Body.String())
					}
				})
			}
			if basePath != "" {
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
				if resp.Code != http.StatusNotFound {
					t.Fatalf("unprefixed version status = %d", resp.Code)
				}
			}
		})
	}
}

func TestStatusOmitsVersionFields(t *testing.T) {
	previousVersion := version.Version
	t.Cleanup(func() { version.Version = previousVersion })
	version.Version = "v1.2.3"

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if _, ok := body["version"]; ok {
		t.Fatalf("expected status response to omit version, got %+v", body)
	}
	if _, ok := body["updateCheckEnabled"]; ok {
		t.Fatalf("expected status response to omit updateCheckEnabled, got %+v", body)
	}
}

func TestStatusReturnsCPAPublicURL(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{
		Status: StatusRouteConfig{CPAPublicURL: "https://cpa.public.example.com/"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"cpa_public_url":"https://cpa.public.example.com/"`) {
		t.Fatalf("expected CPA public URL in status response, got %s", body)
	}
	if contains(body, "cpa_management_url") {
		t.Fatalf("expected status response to use cpa_public_url instead of cpa_management_url, got %s", body)
	}
}

func TestStatusOmitsCPAPublicURLWhenUnset(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{
		Status: StatusRouteConfig{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if body := resp.Body.String(); contains(body, "cpa_public_url") || contains(body, "cpa_management_url") {
		t.Fatalf("expected status response to omit CPA browser URL fields when unset, got %s", body)
	}
}

func TestVersionHidesUpdateCheckForDevVersion(t *testing.T) {
	previousVersion := version.Version
	t.Cleanup(func() { version.Version = previousVersion })
	version.Version = "dev"

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	var body routerVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Version != "dev" || body.UpdateCheckEnabled {
		t.Fatalf("unexpected response body: %+v", body)
	}
}

func TestManualSyncRouteIsNotRegistered(t *testing.T) {
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.Code)
	}
}

func TestSubpathRoutesOnlyServePrefixedEndpoints(t *testing.T) {
	router := NewRouter(nil, statusStub{status: poller.Status{
		Running: true,
	}}, nil, nil, AuthConfig{BasePath: "/cpa"}, nil, "/cpa")

	for _, testCase := range []struct {
		path       string
		statusCode int
	}{
		{path: "/cpa/healthz", statusCode: http.StatusOK},
		{path: "/cpa/api/v1/status", statusCode: http.StatusOK},
		{path: "/cpa/api/v1/version", statusCode: http.StatusOK},
		{path: "/healthz", statusCode: http.StatusNotFound},
		{path: "/api/v1/status", statusCode: http.StatusNotFound},
		{path: "/api/v1/version", statusCode: http.StatusNotFound},
	} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
		router.ServeHTTP(resp, req)
		if resp.Code != testCase.statusCode {
			t.Fatalf("expected %s to return %d, got %d", testCase.path, testCase.statusCode, resp.Code)
		}
	}
}

func TestSubpathStaticRoutesServeOnlyUnderPrefix(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html":    `<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`,
		"assets/app.js": "console.log('ok')",
	})

	router := NewRouter(staticFS, nil, nil, nil, AuthConfig{BasePath: "/cpa"}, nil, "/cpa")

	for _, testCase := range []struct {
		path       string
		statusCode int
		contains   string
	}{
		{path: "/cpa/", statusCode: http.StatusOK, contains: `window.__APP_BASE_PATH__ = "/cpa";`},
		{path: "/cpa/dashboard", statusCode: http.StatusOK, contains: `window.__APP_BASE_PATH__ = "/cpa";`},
		{path: "/cpa/assets/app.js", statusCode: http.StatusOK, contains: "console.log('ok')"},
		{path: "/cpa/missing.html", statusCode: http.StatusOK, contains: `window.__APP_BASE_PATH__ = "/cpa";`},
		{path: "/foo", statusCode: http.StatusNotFound},
		{path: "/assets/app.js", statusCode: http.StatusNotFound},
		{path: "/cpa/api/unknown", statusCode: http.StatusNotFound},
	} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
		router.ServeHTTP(resp, req)
		if resp.Code != testCase.statusCode {
			t.Fatalf("expected %s to return %d, got %d", testCase.path, testCase.statusCode, resp.Code)
		}
		if testCase.contains != "" && !contains(resp.Body.String(), testCase.contains) {
			t.Fatalf("expected %s response to contain %q, got %s", testCase.path, testCase.contains, resp.Body.String())
		}
	}
}

func TestCleanURLPathUsesSlashSemantics(t *testing.T) {
	if cleaned := cleanURLPath("/cpa//dashboard/../assets/app.js"); cleaned != "/cpa/assets/app.js" {
		t.Fatalf("expected slash-normalized URL path, got %q", cleaned)
	}
}

func TestStaticAssetPathRejectsBackslashTraversal(t *testing.T) {
	if _, ok := staticAssetPath(`/..\.env`); ok {
		t.Fatal("expected backslash traversal path to be rejected")
	}
}

func TestRootStaticRouteInjectsEmptyBasePath(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html": `<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`,
	})

	router := NewRouter(staticFS, nil, nil, nil, AuthConfig{}, nil, "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if !contains(resp.Body.String(), `window.__APP_BASE_PATH__ = "";`) {
		t.Fatalf("expected injected empty base path, got %s", resp.Body.String())
	}
}

func TestStaticResponsesUseContentAppropriateCaching(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html":    `<html><body>app</body></html>`,
		"assets/app.js": "console.log('ok')",
	})
	router := NewRouter(staticFS, nil, nil, nil, AuthConfig{}, nil, "/cpa")
	for _, tc := range []struct{ path, cache string }{
		{"/cpa/dashboard", "no-store"},
		{"/cpa/assets/app.js", "public, max-age=31536000, immutable"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if resp.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", resp.Code)
			}
			if got := resp.Header().Get("Cache-Control"); got != tc.cache {
				t.Fatalf("Cache-Control = %q, want %q", got, tc.cache)
			}
		})
	}
}

type routerVersionResponse struct {
	Version            string `json:"version"`
	UpdateCheckEnabled bool   `json:"updateCheckEnabled"`
}

// 路径清洗与反斜线拒绝直接覆盖原函数，避免 HTTP 客户端预先规范化测试输入。
//
//go:linkname cleanURLPath cpa-usage-keeper/internal/api.cleanURLPath
func cleanURLPath(requestPath string) string

//go:linkname staticAssetPath cpa-usage-keeper/internal/api.staticAssetPath
func staticAssetPath(requestPath string) (string, bool)
