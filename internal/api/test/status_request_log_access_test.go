package test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	. "cpa-usage-keeper/internal/api"
)

func TestStatusReturnsCPARequestLogAccessFlag(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{
		Status: StatusRouteConfig{CPARequestLogAccessEnabled: true},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"cpa_request_log_access_enabled":true`) {
		t.Fatalf("expected request log access flag in status response, got %s", body)
	}
}
