package test

import (
	"net/http"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/api"
)

func TestReadinessSeparatesStartupFailureAndRecoveryFromLiveness(t *testing.T) {
	for _, basePath := range []string{"", "/cpa"} {
		t.Run("base="+basePath, func(t *testing.T) {
			config := api.AuthConfig{Enabled: true, BasePath: basePath}
			shell := api.NewStartupShell(nil, config, basePath)
			for _, phase := range []api.StartupPhase{api.StartupPhaseOpening, api.StartupPhaseMigrating, api.StartupPhaseFailed, api.StartupPhaseMigrating} {
				if err := shell.Publish(phase, nil); err != nil {
					t.Fatal(err)
				}
				ready := requestStartup(t, shell, http.MethodGet, basePath+"/readyz")
				if ready.Code != http.StatusServiceUnavailable || ready.Header().Get("Cache-Control") != "no-store" || !strings.Contains(ready.Body.String(), `"phase":"`+string(phase)+`"`) {
					t.Fatalf("%s readiness: code=%d headers=%v body=%s", phase, ready.Code, ready.Header(), ready.Body.String())
				}
				if live := requestStartup(t, shell, http.MethodGet, basePath+"/healthz"); live.Code != http.StatusOK {
					t.Fatalf("%s liveness: %d", phase, live.Code)
				}
			}
			full := api.NewRouter(nil, nil, nil, nil, config, nil, basePath)
			if err := shell.ActivateReady(full); err != nil {
				t.Fatal(err)
			}
			ready := requestStartup(t, shell, http.MethodGet, basePath+"/readyz")
			if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"phase":"ready"`) || ready.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("recovered readiness: code=%d body=%s", ready.Code, ready.Body.String())
			}
			if basePath != "" {
				if outside := requestStartup(t, shell, http.MethodGet, "/readyz"); outside.Code != http.StatusNotFound {
					t.Fatalf("readiness escaped base path: %d", outside.Code)
				}
			}
		})
	}
}

func TestReadinessRedirectsOnlyNoncanonicalGETProbePaths(t *testing.T) {
	staticFS := testStaticFS(t, map[string]string{"index.html": "<html>startup</html>"})
	for _, basePath := range []string{"", "/cpa"} {
		shell := api.NewStartupShell(staticFS, api.AuthConfig{Enabled: true, BasePath: basePath}, basePath)
		canonical := basePath + "/readyz"
		for _, path := range []string{basePath + "//readyz", basePath + "/extra/../readyz"} {
			resp := requestStartup(t, shell, http.MethodGet, path)
			if resp.Code != http.StatusTemporaryRedirect || resp.Header().Get("Location") != canonical || resp.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("noncanonical readiness %s: code=%d headers=%v body=%s", path, resp.Code, resp.Header(), resp.Body.String())
			}
		}
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, basePath + "/extra/../dashboard"},
			{http.MethodPost, basePath + "//readyz"},
		} {
			resp := requestStartup(t, shell, tc.method, tc.path)
			if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "<html>startup</html>") || resp.Header().Get("Location") != "" {
				t.Fatalf("SPA behavior changed for %s %s: code=%d headers=%v body=%s", tc.method, tc.path, resp.Code, resp.Header(), resp.Body.String())
			}
		}
	}
}
