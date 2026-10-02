package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/api"
)

func TestStartupShellServesPublicStatusAndStaticPageWithoutDatabase(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{
		"index.html":    `<html><script>window.__APP_BASE_PATH__ = "__APP_BASE_PATH__";</script></html>`,
		"assets/app.js": "console.log('startup')",
	})
	for _, basePath := range []string{"", "/cpa"} {
		t.Run("base="+basePath, func(t *testing.T) {
			config := api.AuthConfig{BasePath: basePath, FrameAncestorOrigins: []string{"https://cpamc.example"}}
			shell := api.NewStartupShell(staticFS, config, basePath)
			status := requestStartup(t, shell, http.MethodGet, basePath+"/api/v1/startup/status")
			if status.Code != http.StatusOK || status.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("public startup status: code=%d headers=%v", status.Code, status.Header())
			}
			assertStartupJSON(t, status, "opening", true)

			for _, path := range []string{basePath + "/", basePath + "/dashboard/details"} {
				resp := requestStartup(t, shell, http.MethodGet, path)
				if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `window.__APP_BASE_PATH__ = "`+basePath+`";`) {
					t.Fatalf("startup page %s: code=%d body=%s", path, resp.Code, resp.Body.String())
				}
				if got := resp.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self' https://cpamc.example" {
					t.Fatalf("startup embed CSP=%q", got)
				}
				if resp.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("startup HTML must not be cached: %v", resp.Header())
				}
			}
			asset := requestStartup(t, shell, http.MethodGet, basePath+"/assets/app.js")
			if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatalf("startup asset: code=%d headers=%v", asset.Code, asset.Header())
			}
			liveness := requestStartup(t, shell, http.MethodGet, basePath+"/healthz")
			if liveness.Code != http.StatusOK {
				t.Fatalf("startup liveness code=%d", liveness.Code)
			}
			blocked := requestStartup(t, shell, http.MethodPost, basePath+"/api/v1/pricing/models")
			if blocked.Code != http.StatusServiceUnavailable || !strings.Contains(blocked.Body.String(), `"code":"migration_in_progress"`) {
				t.Fatalf("startup business API: code=%d body=%s", blocked.Code, blocked.Body.String())
			}
			if basePath != "" {
				outside := requestStartup(t, shell, http.MethodGet, "/api/v1/startup/status")
				if outside.Code != http.StatusNotFound {
					t.Fatalf("unprefixed status code=%d", outside.Code)
				}
			}
		})
	}
}

func TestStartupShellKeepsFailedPageAndAtomicReadySwitch(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{"index.html": `<html>startup</html>`})
	shell := api.NewStartupShell(staticFS, api.AuthConfig{}, "")
	processed := int64(12)
	if err := shell.Publish(api.StartupPhaseMigrating, &api.StartupProgress{ProcessedCount: processed}); err != nil {
		t.Fatalf("publish migrating: %v", err)
	}
	migrating := requestStartup(t, shell, http.MethodGet, "/api/v1/startup/status")
	assertStartupJSON(t, migrating, "migrating", false)
	if !strings.Contains(migrating.Body.String(), `"processed_count":12,"total_count":null`) {
		t.Fatalf("unknown total must stay null: %s", migrating.Body.String())
	}
	total := int64(20)
	if err := shell.Publish(api.StartupPhaseMigrating, &api.StartupProgress{ProcessedCount: processed, TotalCount: &total}); err != nil {
		t.Fatalf("publish known total: %v", err)
	}
	total = 99
	known := requestStartup(t, shell, http.MethodGet, "/api/v1/startup/status")
	if !strings.Contains(known.Body.String(), `"processed_count":12,"total_count":20`) {
		t.Fatalf("published progress changed after caller mutation: %s", known.Body.String())
	}
	if err := shell.Publish(api.StartupPhaseReady, nil); err == nil {
		t.Fatal("Publish made an empty shell ready")
	}
	if err := shell.Publish(api.StartupPhaseFailed, nil); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	failed := requestStartup(t, shell, http.MethodGet, "/api/v1/startup/status")
	assertStartupJSON(t, failed, "failed", true)
	if strings.Contains(failed.Body.String(), "SQL") || strings.Contains(failed.Body.String(), "database") {
		t.Fatalf("failed status disclosed internals: %s", failed.Body.String())
	}
	if page := requestStartup(t, shell, http.MethodGet, "/pricing"); page.Code != http.StatusOK {
		t.Fatalf("failed shell lost startup page: code=%d", page.Code)
	}
	if liveness := requestStartup(t, shell, http.MethodGet, "/healthz"); liveness.Code != http.StatusOK {
		t.Fatalf("failed shell lost liveness route: code=%d", liveness.Code)
	}
	if blocked := requestStartup(t, shell, http.MethodGet, "/api/v1/pricing/models"); blocked.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed shell exposed business API: code=%d", blocked.Code)
	}
	if err := shell.ActivateReady(nil); err == nil {
		t.Fatal("nil full handler activated ready")
	}
	full := api.NewRouter(staticFS, nil, nil, nil, api.AuthConfig{}, nil, "")
	if err := shell.ActivateReady(full); err != nil {
		t.Fatalf("activate full router: %v", err)
	}
	ready := requestStartup(t, shell, http.MethodGet, "/api/v1/startup/status")
	assertStartupJSON(t, ready, "ready", true)
	if missing := requestStartup(t, shell, http.MethodGet, "/api/v1/unknown"); missing.Code != http.StatusNotFound {
		t.Fatalf("ready router still returned shell error: code=%d", missing.Code)
	}
	if err := shell.Publish(api.StartupPhaseMigrating, nil); err == nil {
		t.Fatal("late startup publisher replaced ready handler")
	}
}

func TestFullRouterStartupStatusIsPublicAndReady(t *testing.T) {
	for _, basePath := range []string{"", "/cpa"} {
		router := api.NewRouter(nil, nil, nil, nil, api.AuthConfig{Enabled: true, BasePath: basePath}, nil, basePath)
		resp := requestStartup(t, router, http.MethodGet, basePath+"/api/v1/startup/status")
		assertStartupJSON(t, resp, "ready", true)
	}
}

func requestStartup(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(method, path, nil))
	return resp
}

func assertStartupJSON(t *testing.T, resp *httptest.ResponseRecorder, phase string, wantNullProgress bool) {
	t.Helper()
	if resp.Code != http.StatusOK {
		t.Fatalf("startup status code=%d body=%s", resp.Code, resp.Body.String())
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode startup status: %v", err)
	}
	if string(value["phase"]) != `"`+phase+`"` || len(value["message"]) == 0 {
		t.Fatalf("unexpected startup status: %s", resp.Body.String())
	}
	if wantNullProgress && string(value["progress"]) != "null" {
		t.Fatalf("unknown progress must be null: %s", resp.Body.String())
	}
}
