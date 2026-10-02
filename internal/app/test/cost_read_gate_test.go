package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppSharesCostReadGateWithRouter(t *testing.T) {
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	cfg.AppBasePath = "/keeper"
	application := newDatabasePoolTestApp(t, cfg)
	if application.CostReadGate == nil {
		t.Fatal("App did not construct a shared cost read gate")
	}
	resume, err := application.CostReadGate.BlockAndDrain(context.Background())
	if err != nil {
		t.Fatalf("block App cost reads: %v", err)
	}
	t.Cleanup(resume)

	request := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/keeper/api/v1"+path, nil))
		return response
	}
	if response := request("/usage/overview?range=24h"); response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"costs_busy"`) {
		t.Fatalf("App fee route did not use shared gate: %d %s", response.Code, response.Body.String())
	}
	if response := request("/usage/activity?range=24h"); response.Code == http.StatusServiceUnavailable && strings.Contains(response.Body.String(), "costs_busy") {
		t.Fatalf("App non-fee route was blocked: %d %s", response.Code, response.Body.String())
	}
	resume()
	if response := request("/usage/overview?range=24h"); response.Code != http.StatusOK {
		t.Fatalf("App fee route did not resume: %d %s", response.Code, response.Body.String())
	}
}
