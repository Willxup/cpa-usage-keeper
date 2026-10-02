package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestCostReadRoutesRespectAuthAndExactFeeScope(t *testing.T) {
	gate := service.NewCostReadGate()
	resume, err := gate.BlockAndDrain(context.Background())
	if err != nil {
		t.Fatalf("block fee reads: %v", err)
	}
	t.Cleanup(resume)
	basePath := "/keeper"
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: basePath}
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), basePath, keeperapi.OptionalProviders{
		CostReadGate: gate,
		CPAAPIKeys:   &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-viewer", DisplayKey: "sk-...viewer"}},
	})
	admin := &http.Cookie{Name: standardSessionCookieName, Value: adminToken}
	viewer := &http.Cookie{Name: standardSessionCookieName, Value: viewerToken}
	for _, test := range []struct {
		name, method, path string
		cookie             *http.Cookie
	}{
		{"overview", http.MethodGet, "/usage/overview", admin},
		{"comparisons", http.MethodGet, "/usage/overview/comparisons", admin},
		{"realtime", http.MethodGet, "/usage/overview/realtime", admin},
		{"analysis", http.MethodGet, "/usage/analysis", admin},
		{"events", http.MethodGet, "/usage/events", admin},
		{"CSV export", http.MethodGet, "/usage/events/export?format=csv", admin},
		{"JSON export", http.MethodGet, "/usage/events/export?format=json", admin},
		{"quota history", http.MethodGet, "/quota/history/auth-1", admin},
		{"quota cache", http.MethodPost, "/quota/cache", admin},
		{"quota refresh", http.MethodPost, "/quota/refresh", admin},
		{"quota refresh result", http.MethodGet, "/quota/refresh/auth-1", admin},
		{"viewer overview", http.MethodGet, "/key-overview", viewer},
		{"viewer comparisons", http.MethodGet, "/key-overview/comparisons", viewer},
		{"viewer realtime", http.MethodGet, "/key-overview/realtime", viewer},
		{"viewer analysis", http.MethodGet, "/key-analysis", viewer},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serveCostReadGateRequest(router, test.method, basePath+"/api/v1"+test.path, test.cookie)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected costs_busy 503, got %d %s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode busy response: %v", err)
			}
			if len(body) != 2 || body["code"] != "costs_busy" || body["message"] == "" {
				t.Fatalf("busy response exposed extra state or missed summary: %+v", body)
			}
		})
	}

	for _, test := range []struct {
		name, method, path string
		cookie             *http.Cookie
	}{
		{"activity", http.MethodGet, "/usage/activity", admin},
		{"latency", http.MethodGet, "/usage/analysis/latency", admin},
		{"identities", http.MethodGet, "/usage/identities", admin},
		{"credential list and health", http.MethodGet, "/usage/identities/page", admin},
		{"event filters", http.MethodGet, "/usage/events/filters/models", admin},
		{"request log", http.MethodGet, "/usage/events/1/request-log", admin},
		{"pricing", http.MethodGet, "/pricing", admin},
		{"quota inspection", http.MethodGet, "/quota/inspection", admin},
		{"quota settings", http.MethodGet, "/quota/auto-refresh/settings", admin},
		{"viewer activity", http.MethodGet, "/key-activity", viewer},
		{"viewer latency", http.MethodGet, "/key-analysis/latency", viewer},
	} {
		t.Run("non-fee "+test.name, func(t *testing.T) {
			response := serveCostReadGateRequest(router, test.method, basePath+"/api/v1"+test.path, test.cookie)
			if response.Code == http.StatusServiceUnavailable && strings.Contains(response.Body.String(), "costs_busy") {
				t.Fatalf("non-fee route was blocked: %s", response.Body.String())
			}
		})
	}
	if response := serveCostReadGateRequest(router, http.MethodGet, basePath+"/api/v1/usage/overview", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request saw gate before auth: %d %s", response.Code, response.Body.String())
	}
	if response := serveCostReadGateRequest(router, http.MethodGet, basePath+"/api/v1/usage/overview", viewer); response.Code != http.StatusForbidden {
		t.Fatalf("viewer reached admin cost gate: %d %s", response.Code, response.Body.String())
	}
	if response := serveCostReadGateRequest(router, http.MethodGet, basePath+"/api/v1/key-overview", admin); response.Code != http.StatusForbidden {
		t.Fatalf("admin reached viewer cost gate: %d %s", response.Code, response.Body.String())
	}
}

func TestCostReadGateWaitsForCompleteCSVAndJSONExports(t *testing.T) {
	for _, format := range []string{"csv", "json"} {
		t.Run(format, func(t *testing.T) {
			gate := service.NewCostReadGate()
			router := keeperapi.NewRouter(nil, nil, &usageEventsStub{}, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CostReadGate: gate})
			recorder := &blockingCostReadExportWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
			releaseOutput := sync.OnceFunc(func() { close(recorder.release) })
			t.Cleanup(releaseOutput)
			exportDone := make(chan struct{})
			go func() {
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/export?range=24h&format="+format, nil))
				close(exportDone)
			}()
			waitForCostReadTestSignal(t, recorder.entered, "export did not reach response writer")

			type drainResult struct {
				resume func()
				err    error
			}
			drained := make(chan drainResult, 1)
			go func() {
				resume, err := gate.BlockAndDrain(context.Background())
				drained <- drainResult{resume: resume, err: err}
			}()
			waitForAPICostGateBlocked(t, gate)
			select {
			case result := <-drained:
				t.Fatalf("drain returned before export output finished: %+v", result)
			case <-time.After(20 * time.Millisecond):
			}
			if response := serveCostReadGateRequest(router, http.MethodGet, "/api/v1/usage/overview", nil); response.Code != http.StatusServiceUnavailable {
				t.Fatalf("new fee read was not deferred: %d %s", response.Code, response.Body.String())
			}
			releaseOutput()
			waitForCostReadTestSignal(t, exportDone, "export did not complete after output release")
			if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
				t.Fatalf("export did not produce complete output: %d %s", recorder.Code, recorder.Body.String())
			}
			select {
			case result := <-drained:
				if result.err != nil || result.resume == nil {
					t.Fatalf("drain failed after export completion: %v", result.err)
				}
				result.resume()
			case <-time.After(time.Second):
				t.Fatal("drain did not finish after export completion")
			}
			if response := serveCostReadGateRequest(router, http.MethodGet, "/api/v1/usage/overview", nil); response.Code == http.StatusServiceUnavailable {
				t.Fatalf("fee reads did not resume: %s", response.Body.String())
			}
		})
	}
}

type blockingCostReadExportWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingCostReadExportWriter) Write(data []byte) (int, error) {
	w.once.Do(func() {
		close(w.entered)
		<-w.release
	})
	return w.ResponseRecorder.Write(data)
}

func serveCostReadGateRequest(router http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if method != http.MethodGet {
		request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func waitForAPICostGateBlocked(t *testing.T, gate *service.CostReadGate) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		release, ok := gate.Acquire()
		if !ok {
			return
		}
		release()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fee reads were not blocked")
}

func waitForCostReadTestSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}
