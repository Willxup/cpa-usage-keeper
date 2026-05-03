package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
)

type statusStub struct {
	status poller.Status
}

type syncStatusStub struct {
	status poller.Status
	calls  int
	err    error
}

type userFacingSyncError struct {
	message string
}

func (e userFacingSyncError) Error() string {
	return e.message
}

func (e userFacingSyncError) UserMessage() string {
	return e.message
}

func (s statusStub) Status() poller.Status {
	return s.status
}

func (s *syncStatusStub) Status() poller.Status {
	return s.status
}

func (s *syncStatusStub) SyncNow(context.Context) error {
	s.calls++
	return s.err
}

func TestHealthzReturnsOK(t *testing.T) {
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
}

func TestStatusReturnsPollerState(t *testing.T) {
	lastRunAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	router := NewRouter("", statusStub{status: poller.Status{
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
	if !(contains(body, `"running":true`) && contains(body, `"sync_running":false`) && contains(body, `"last_error":"boom"`) && contains(body, `"last_warning":"metadata unavailable"`) && contains(body, `"last_status":"completed_with_warnings"`) && contains(body, `"last_run_at":"2026-04-16T12:00:00Z"`)) {
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

	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "")
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
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil, "")
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

func TestManualSyncTriggersSyncRunner(t *testing.T) {
	lastRunAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	syncer := &syncStatusStub{status: poller.Status{Running: true, LastRunAt: lastRunAt, LastStatus: "completed"}}
	router := NewRouter("", syncer, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if syncer.calls != 1 {
		t.Fatalf("expected sync runner to be called once, got %d", syncer.calls)
	}
	body := resp.Body.String()
	if !(contains(body, `"last_status":"completed"`) && contains(body, `"last_run_at":"2026-04-16T12:00:00Z"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestManualSyncReturnsConflictWhenAlreadyRunning(t *testing.T) {
	syncer := &syncStatusStub{err: poller.ErrSyncAlreadyRunning}
	router := NewRouter("", syncer, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.Code)
	}
}

func TestManualSyncReturnsWarningsAsError(t *testing.T) {
	syncer := &syncStatusStub{
		status: poller.Status{LastStatus: "completed_with_warnings", LastWarning: "metadata unavailable"},
		err:    poller.ErrSyncCompletedWithWarnings,
	}
	router := NewRouter("", syncer, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"error":"manual sync failed"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestManualSyncReturnsUserFacingStageError(t *testing.T) {
	syncer := &syncStatusStub{err: userFacingSyncError{message: "metadata sync failed"}}
	router := NewRouter("", syncer, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"error":"metadata sync failed"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestManualSyncRateLimitsRepeatedRequests(t *testing.T) {
	syncer := &syncStatusStub{status: poller.Status{LastStatus: "completed"}}
	router := NewRouter("", syncer, nil, nil, AuthConfig{}, nil, "")

	firstResp := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("expected first sync to return 200, got %d", firstResp.Code)
	}

	secondResp := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	router.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second sync within one second to return 429, got %d", secondResp.Code)
	}
	if syncer.calls != 1 {
		t.Fatalf("expected rate-limited sync not to call runner again, got %d calls", syncer.calls)
	}
}

func TestSubpathRoutesOnlyServePrefixedEndpoints(t *testing.T) {
	lastRunAt := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	router := NewRouter("", statusStub{status: poller.Status{
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
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(`<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Mkdir(filepath.Join(staticDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	router := NewRouter(staticDir, nil, nil, nil, AuthConfig{BasePath: "/cpa"}, nil, "/cpa")

	for _, testCase := range []struct {
		path       string
		statusCode int
		contains   string
	}{
		{path: "/cpa/", statusCode: http.StatusOK, contains: `window.__APP_BASE_PATH__ = "/cpa";`},
		{path: "/cpa/dashboard", statusCode: http.StatusOK, contains: `window.__APP_BASE_PATH__ = "/cpa";`},
		{path: "/cpa/assets/app.js", statusCode: http.StatusOK, contains: "console.log('ok')"},
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
	staticDir := t.TempDir()

	if _, ok := staticAssetPath(staticDir, `/..\.env`); ok {
		t.Fatal("expected backslash traversal path to be rejected")
	}
}

func TestStaticFallbackRejectsBackslashPath(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("index page"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, `..\.env`), []byte("sentinel secret"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	router := NewRouter(staticDir, nil, nil, nil, AuthConfig{}, nil, "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, `/..\.env`, nil)
	router.ServeHTTP(resp, req)

	if contains(resp.Body.String(), "sentinel secret") {
		t.Fatalf("expected backslash path not to serve sentinel file, got %s", resp.Body.String())
	}
	if !contains(resp.Body.String(), "index page") {
		t.Fatalf("expected SPA fallback response, got %s", resp.Body.String())
	}
}

func TestRootStaticRouteInjectsEmptyBasePath(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(`<html><head><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></head><body>app</body></html>`), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	router := NewRouter(staticDir, nil, nil, nil, AuthConfig{}, nil, "")
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
