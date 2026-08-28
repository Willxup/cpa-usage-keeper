package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type errorEventsStub struct {
	request  service.ErrorEventListRequest
	response service.ErrorEventListResponse
	err      error
}

func (s *errorEventsStub) StoreErrorEvent(context.Context, string, time.Time) error { return nil }

func (s *errorEventsStub) ListErrorEvents(_ context.Context, request service.ErrorEventListRequest) (service.ErrorEventListResponse, error) {
	s.request = request
	return s.response, s.err
}

func TestErrorEventsRouteReturnsSafeDisplayPayloadAndCursor(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 12, 0, 0, 0, time.Local)
	credentialRetry := timestamp.Add(5 * time.Minute)
	modelRetry := timestamp.Add(3 * time.Minute)
	quotaExceeded := true
	quotaReason := "limit for sk-secret-value"
	backoff := 2
	modelName := "gpt-5.6"
	modelStatus := "error"
	modelMessage := "Bearer abc.def.secret"
	modelUnavailable := true
	provider := &errorEventsStub{response: service.ErrorEventListResponse{
		Events: []entities.ErrorEvent{{
			ID: 9, Timestamp: timestamp, Provider: "codex", Model: "gpt-5.6", AuthID: "runtime-auth-id", AuthIndex: "identity-auth-index",
			StatusCode: 429, Body: "upstream rejected api_key=sk-secret-value at base_url=https://private.example/v1", Code: "rate_limit", Retryable: true,
			AuthStatus: "error", AuthStatusMessage: "hidden auth snapshot", AuthUnavailable: true, AuthNextRetryAfter: &credentialRetry,
			AuthQuotaExceeded: &quotaExceeded, AuthQuotaReason: &quotaReason, AuthQuotaBackoffLevel: &backoff,
			AuthModelName: &modelName, AuthModelStatus: &modelStatus, AuthModelStatusMessage: &modelMessage, AuthModelUnavailable: &modelUnavailable,
			AuthModelNextRetryAfter: &modelRetry,
		}},
		HasMore: true,
		APIKey:  "sk-secret-value",
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{ErrorEvents: provider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/42/errors?page_size=50", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	assertNoStoreHeaders(t, resp)
	body := resp.Body.String()
	for _, secret := range []string{"sk-secret-value", "hidden auth snapshot", `"auth_id"`, `"auth_index"`, `"auth_status"`} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "base_url=https://private.example/v1") {
		t.Fatalf("response removed non-API-key error context: %s", body)
	}
	var payload struct {
		Events []struct {
			ID                   string  `json:"id"`
			StatusCode           int     `json:"status_code"`
			BodySummary          string  `json:"body_summary"`
			CredentialRetryAfter *string `json:"credential_retry_after"`
			ModelRetryAfter      *string `json:"model_retry_after"`
		} `json:"events"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Events) != 1 || payload.Events[0].ID != "9" || payload.Events[0].StatusCode != 429 || payload.Events[0].CredentialRetryAfter == nil || payload.Events[0].ModelRetryAfter == nil {
		t.Fatalf("unexpected response: %+v", payload)
	}
	if !payload.HasMore || payload.NextCursor == "" || provider.request.IdentityID != 42 || provider.request.PageSize != 50 {
		t.Fatalf("unexpected pagination/request: payload=%+v request=%+v", payload, provider.request)
	}
}

func TestErrorEventsRouteOnlyRedactsKnownAPIKey(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 12, 0, 0, 0, time.Local)
	provider := &errorEventsStub{response: service.ErrorEventListResponse{
		Events: []entities.ErrorEvent{{
			ID: 10, Timestamp: timestamp, Provider: "codex", Model: "gpt-5.6", StatusCode: 401,
			Body: `{
	  "model": "gpt-5.6",
	  "status": "error",
	  "code": "unauthorized",
	  "message": "API key generic-api-key was rejected",
	  "token": "provider-error-token-name",
	  "claims": {"aud": "provider-error-audience"},
	  "config": {"region": "provider-error-region"}
}`,
			Code: "unauthorized", AuthStatus: "error",
		}},
		APIKey: "generic-api-key",
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{ErrorEvents: provider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/42/errors", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, "generic-api-key") {
		t.Fatalf("response leaked the known API key: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("response did not contain redaction marker: %s", body)
	}
	for _, ordinaryValue := range []string{
		"gpt-5.6",
		"error",
		"unauthorized",
		"provider-error-token-name",
		"provider-error-audience",
		"provider-error-region",
	} {
		if !strings.Contains(body, ordinaryValue) {
			t.Fatalf("response removed non-API-key error value %q: %s", ordinaryValue, body)
		}
	}
}

func TestErrorEventsRouteParsesCursorAndErrors(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 12, 0, 0, 0, time.Local)
	cursor := base64.RawURLEncoding.EncodeToString([]byte(timestamp.Format(time.RFC3339Nano) + "|17"))
	provider := &errorEventsStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{ErrorEvents: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/42/errors?cursor="+cursor, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || provider.request.Cursor == nil || provider.request.Cursor.ID != 17 || !provider.request.Cursor.Timestamp.Equal(timestamp) {
		t.Fatalf("cursor was not parsed: status=%d request=%+v body=%s", resp.Code, provider.request, resp.Body.String())
	}

	for _, path := range []string{
		"/api/v1/usage/identities/not-an-id/errors",
		"/api/v1/usage/identities/42/errors?cursor=invalid",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, resp.Code)
		}
	}

	provider.err = gorm.ErrRecordNotFound
	req = httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/404/errors", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, body=%s", resp.Code, resp.Body.String())
	}

	provider.err = errors.New("database unavailable")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/42/errors", nil)
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("internal error status = %d, body=%s", resp.Code, resp.Body.String())
	}
}

var _ service.ErrorEventProvider = (*errorEventsStub)(nil)
var _ = repository.ErrorEventCursor{}
