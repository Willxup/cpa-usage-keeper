package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
)

type statusStub struct {
	status poller.Status
}

func (s statusStub) Status() poller.Status {
	return s.status
}

func TestHealthzReturnsOK(t *testing.T) {
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil)
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
	}}, nil, nil, AuthConfig{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"running":true`) && contains(body, `"sync_running":false`) && contains(body, `"last_error":"boom"`) && contains(body, `"last_run_at":"2026-04-16T12:00:00Z"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestStatusReturnsEmptyStateWithoutProvider(t *testing.T) {
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if body := resp.Body.String(); body != "{\"running\":false,\"sync_running\":false}" {
		t.Fatalf("unexpected response body: %s", body)
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
