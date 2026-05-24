package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

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
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	lastRunAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	router := NewRouter(nil, statusStub{status: poller.Status{
		Running:     true,
		SyncRunning: false,
		LastRunAt:   lastRunAt,
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
	if !(contains(body, `"running":true`) && contains(body, `"sync_running":false`) && contains(body, `"last_error":"boom"`) && contains(body, `"last_warning":"metadata unavailable"`) && contains(body, `"last_status":"completed_with_warnings"`) && contains(body, `"last_run_at":"2026-04-16T20:00:00+08:00"`)) {
		t.Fatalf("unexpected response body: %s", body)
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

func TestStatusReturnsVersionAndUpdateCheckFlag(t *testing.T) {
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
	body := resp.Body.String()
	if !contains(body, `"version":"v1.2.3"`) || !contains(body, `"updateCheckEnabled":true`) {
		t.Fatalf("unexpected response body: %s", body)
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

func TestStatusHidesUpdateCheckForDevVersion(t *testing.T) {
	previousVersion := version.Version
	t.Cleanup(func() { version.Version = previousVersion })
	version.Version = "dev"

	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"version":"dev"`) || !contains(body, `"updateCheckEnabled":false`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestManualSyncRouteIsNotRegistered(t *testing.T) {
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.Code)
	}
}

func TestSubpathRoutesOnlyServePrefixedEndpoints(t *testing.T) {
	lastRunAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	router := NewRouter(nil, statusStub{status: poller.Status{
		Running:   true,
		LastRunAt: lastRunAt,
	}}, nil, nil, AuthConfig{BasePath: "/cpa"}, nil, "/cpa")

	for _, testCase := range []struct {
		path       string
		statusCode int
	}{
		{path: "/cpa/healthz", statusCode: http.StatusOK},
		{path: "/cpa/api/v1/status", statusCode: http.StatusOK},
		{path: "/healthz", statusCode: http.StatusNotFound},
		{path: "/api/v1/status", statusCode: http.StatusNotFound},
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

func TestStaticHTMLResponsesBypassCache(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html":    `<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`,
		"assets/app.js": "console.log('ok')",
	})

	router := NewRouter(staticFS, nil, nil, nil, AuthConfig{}, nil, "/cpa")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cpa/dashboard", nil)
	router.ServeHTTP(resp, req)

	if got := resp.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected HTML Cache-Control no-store, got %q", got)
	}
}

func TestStaticAssetResponsesUseLongCache(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html":    `<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`,
		"assets/app.js": "console.log('ok')",
	})

	router := NewRouter(staticFS, nil, nil, nil, AuthConfig{}, nil, "/cpa")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cpa/assets/app.js", nil)
	router.ServeHTTP(resp, req)

	if got := resp.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("expected asset Cache-Control immutable cache, got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool { return stringContains(s, sub) })())
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
