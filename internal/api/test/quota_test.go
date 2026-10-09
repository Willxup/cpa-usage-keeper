package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/quota"
)

type quotaProviderStub struct {
	QuotaProvider
	historyRequest           quota.CodexQuotaHistoryRequest
	historyResponse          quota.CodexQuotaHistoryResponse
	historyErr               error
	resetRequest             quota.ResetRequest
	resetResponse            quota.ResetResponse
	resetErr                 error
	refreshRequest           quota.RefreshRequest
	refreshResponse          quota.RefreshResponse
	refreshErr               error
	taskAuthIndex            string
	taskResponse             quota.RefreshTaskResponse
	taskErr                  error
	cacheRequest             quota.CacheRequest
	cacheResponse            quota.CacheResponse
	cacheErr                 error
	inspectionStatusResponse quota.InspectionStatus
	inspectionStatusErr      error
	inspectionStartResponse  quota.InspectionStatus
	inspectionStartErr       error
	inspectionStatusCalls    int
	inspectionStartCalls     int
}

func (s *quotaProviderStub) DeleteCodexQuotaHistoryCycle(context.Context, string, int64) error {
	return nil
}

func (s *quotaProviderStub) GetCodexQuotaHistory(ctx context.Context, request quota.CodexQuotaHistoryRequest) (quota.CodexQuotaHistoryResponse, error) {
	s.historyRequest = request
	return s.historyResponse, s.historyErr
}

func (s *quotaProviderStub) Refresh(ctx context.Context, request quota.RefreshRequest) (quota.RefreshResponse, error) {
	s.refreshRequest = request
	return s.refreshResponse, s.refreshErr
}

func (s *quotaProviderStub) GetRefreshTaskByAuthIndex(ctx context.Context, authIndex string) (quota.RefreshTaskResponse, error) {
	s.taskAuthIndex = authIndex
	return s.taskResponse, s.taskErr
}

func (s *quotaProviderStub) GetCachedQuota(ctx context.Context, request quota.CacheRequest) (quota.CacheResponse, error) {
	s.cacheRequest = request
	return s.cacheResponse, s.cacheErr
}

func (s *quotaProviderStub) GetInspectionStatus(ctx context.Context) (quota.InspectionStatus, error) {
	s.inspectionStatusCalls++
	return s.inspectionStatusResponse, s.inspectionStatusErr
}

func (s *quotaProviderStub) Reset(ctx context.Context, request quota.ResetRequest) (quota.ResetResponse, error) {
	s.resetRequest = request
	return s.resetResponse, s.resetErr
}

func (s *quotaProviderStub) StartInspection(ctx context.Context) (quota.InspectionStatus, error) {
	s.inspectionStartCalls++
	return s.inspectionStartResponse, s.inspectionStartErr
}

func TestCodexQuotaHistoryForwardsWindowRoleSelection(t *testing.T) {
	generatedAt := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	provider := &quotaProviderStub{historyResponse: quota.CodexQuotaHistoryResponse{
		GeneratedAt: generatedAt,
		RangeStart:  generatedAt.Add(-30 * 24 * time.Hour),
		Cycles: []quota.CodexQuotaHistoryCycle{{
			ID:                 1,
			Status:             "current",
			WindowSeconds:      604800,
			WindowStartedAt:    generatedAt.Add(-24 * time.Hour),
			ResetAt:            generatedAt.Add(6 * 24 * time.Hour),
			EffectiveStartedAt: generatedAt.Add(-24 * time.Hour),
			EffectiveEndedAt:   generatedAt.Add(6 * 24 * time.Hour),
		}},
		Windows: []quota.CodexQuotaHistoryWindow{{
			WindowRole:      "secondary",
			WindowSeconds:   604800,
			HasCurrentCycle: true,
			LastObservedAt:  generatedAt,
		}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	resp := serveAPIGet(router, "/api/v1/quota/history/codex-auth?window_role=secondary")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.historyRequest.AuthIndex != "codex-auth" || provider.historyRequest.WindowRole == nil || *provider.historyRequest.WindowRole != "secondary" {
		t.Fatalf("unexpected quota history request: %+v", provider.historyRequest)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"generated_at":"2026-08-21T12:00:00Z"`) || !strings.Contains(body, `"window_role":"secondary"`) || !strings.Contains(body, `"window_seconds":604800`) || !strings.Contains(body, `"effective_started_at":"2026-08-20T12:00:00Z"`) || !strings.Contains(body, `"effective_ended_at":"2026-08-27T12:00:00Z"`) {
		t.Fatalf("unexpected quota history response: %s", body)
	}
}

func TestCodexQuotaHistoryMapsValidationAndIdentityErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		err        error
		wantStatus int
	}{
		{name: "service validation", path: "/api/v1/quota/history/codex-auth?window_role=primary", err: quota.ErrValidation, wantStatus: http.StatusBadRequest},
		{name: "unsupported identity", path: "/api/v1/quota/history/codex-auth", err: quota.ErrUnsupportedType, wantStatus: http.StatusBadRequest},
		{name: "missing identity", path: "/api/v1/quota/history/codex-auth", err: quota.ErrNotFound, wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &quotaProviderStub{historyErr: test.err}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
			resp := serveAPIGet(router, test.path)
			if resp.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", test.wantStatus, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestQuotaCacheReturnsCachedCurrentPageQuota(t *testing.T) {
	refreshedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{
		Items: []quota.CachedQuotaItem{{AuthIndex: "auth-1", FileName: new("claude-user.json"), Status: quota.RefreshTaskStatusCompleted, RefreshedAt: &refreshedAt, Quota: &quota.CheckResponse{ID: "auth-1", Subscription: &quota.SubscriptionInfo{Provider: "codex", Plan: "plus"}, Quota: []quota.QuotaRow{{Key: "rate_limit.secondary_window", Label: "Weekly"}}}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/cache", `{"auth_indexes":["auth-1","auth-2"]}`)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := strings.Join(provider.cacheRequest.AuthIndexes, ","); got != "auth-1,auth-2" {
		t.Fatalf("expected auth indexes to be forwarded, got %+v", provider.cacheRequest.AuthIndexes)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"file_name":"claude-user.json"`) || !strings.Contains(body, `"refreshed_at":"2026-05-26T12:00:00Z"`) || strings.Contains(body, `"updated_at"`) || !strings.Contains(body, `"id":"auth-1"`) || !strings.Contains(body, `"label":"Weekly"`) || !strings.Contains(body, `"subscription":{"provider":"codex","plan":"plus"}`) || strings.Contains(body, `"planType"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaCacheAllowsMoreThanRefreshLimit(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	authIndexes := make([]string, 21)
	for i := range authIndexes {
		authIndexes[i] = "auth-" + strconv.Itoa(i+1)
	}
	bodyBytes, err := json.Marshal(map[string]any{"auth_indexes": authIndexes})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/cache", string(bodyBytes))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(provider.cacheRequest.AuthIndexes) != 21 {
		t.Fatalf("expected cache request to use all requested auth indexes, got %+v", provider.cacheRequest)
	}
}

func TestQuotaInspectionStatusReturnsSummary(t *testing.T) {
	refreshedAt := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
	completedAt := time.Date(2026, 6, 3, 10, 31, 0, 0, time.UTC)
	provider := &quotaProviderStub{inspectionStatusResponse: quota.InspectionStatus{
		Total: 3, Cached: 2, Running: true, Normal: 1, Unauthorized401: 1, PaymentRequired402: 1, Unauthorized401402: 2, CompletedAt: &completedAt,
		Results: []quota.InspectionResult{{AuthIndex: "auth-1", Name: "Claude Main", Type: "claude", FileName: new("claude-user.json"), Status: quota.InspectionResultStatusNormal, RefreshedAt: &refreshedAt}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveAPIGet(router, "/api/v1/quota/inspection")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.inspectionStatusCalls != 1 || provider.inspectionStartCalls != 0 {
		t.Fatalf("expected status lookup only, got status=%d start=%d", provider.inspectionStatusCalls, provider.inspectionStartCalls)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"total":3`) || !strings.Contains(body, `"cached":2`) || !strings.Contains(body, `"unauthorized_401_402":2`) || !strings.Contains(body, `"completed_at":"2026-06-03T10:31:00Z"`) || !strings.Contains(body, `"auth_index":"auth-1"`) || !strings.Contains(body, `"file_name":"claude-user.json"`) || !strings.Contains(body, `"refreshed_at":"2026-06-03T10:30:00Z"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if strings.Contains(body, `"provider"`) {
		t.Fatalf("expected inspection response to use type/name only, got %s", body)
	}
}

func TestQuotaInspectionStartReturnsFreshStatus(t *testing.T) {
	provider := &quotaProviderStub{inspectionStartResponse: quota.InspectionStatus{Total: 2, Cached: 0, Running: true}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/inspection", nil)

	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.inspectionStartCalls != 1 || provider.inspectionStatusCalls != 0 {
		t.Fatalf("expected inspection start only, got start=%d status=%d", provider.inspectionStartCalls, provider.inspectionStatusCalls)
	}
	if body := resp.Body.String(); !strings.Contains(body, `"total":2`) || !strings.Contains(body, `"cached":0`) || !strings.Contains(body, `"running":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshCreatesTasksForCurrentPageAuthIndexes(t *testing.T) {
	provider := &quotaProviderStub{refreshResponse: quota.RefreshResponse{
		Tasks:    []quota.RefreshTaskRef{{AuthIndex: "auth-1"}, {AuthIndex: "auth-2"}},
		Accepted: 2,
		Limit:    2,
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/refresh", `{"auth_indexes":["auth-1","auth-2"]}`)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := strings.Join(provider.refreshRequest.AuthIndexes, ","); got != "auth-1,auth-2" {
		t.Fatalf("expected auth indexes to be forwarded, got %+v", provider.refreshRequest.AuthIndexes)
	}
	if provider.refreshRequest.Source != quota.RefreshSourceManual {
		t.Fatalf("expected manual refresh source, got %q", provider.refreshRequest.Source)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"tasks"`) || !strings.Contains(body, `"authIndex":"auth-1"`) || strings.Contains(body, `"taskId"`) || !strings.Contains(body, `"accepted":2`) || !strings.Contains(body, `"limit":2`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshAllowsCurrentPageSizeWithoutOuterTwentyLimit(t *testing.T) {
	provider := &quotaProviderStub{refreshResponse: quota.RefreshResponse{Accepted: 25, Limit: 25}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	authIndexes := make([]string, 25)
	for i := range authIndexes {
		authIndexes[i] = "auth"
	}
	bodyBytes, err := json.Marshal(map[string]any{"auth_indexes": authIndexes})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/refresh", string(bodyBytes))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(provider.refreshRequest.AuthIndexes) != 25 {
		t.Fatalf("expected refresh to forward all current-page auth indexes, got %+v", provider.refreshRequest)
	}
}

func TestQuotaRefreshRejectsEmptyAuthIndexes(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/refresh", `{"auth_indexes":[]}`)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.refreshRequest.AuthIndexes != nil {
		t.Fatalf("provider should not be called for empty refresh request, got %+v", provider.refreshRequest)
	}
}

func TestQuotaRefreshTaskReturnsCachedQuotaByAuthIndex(t *testing.T) {
	refreshedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	provider := &quotaProviderStub{taskResponse: quota.RefreshTaskResponse{
		AuthIndex:   "auth-1",
		FileName:    new("claude-user.json"),
		Status:      quota.RefreshTaskStatusCompleted,
		RefreshedAt: &refreshedAt,
		Quota:       &quota.CheckResponse{ID: "auth-1", Subscription: &quota.SubscriptionInfo{Provider: "codex", Plan: "pro-20x"}, Quota: []quota.QuotaRow{{Key: "rate_limit.primary_window", Label: "5h"}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveAPIGet(router, "/api/v1/quota/refresh/auth-1")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.taskAuthIndex != "auth-1" {
		t.Fatalf("expected auth_index to be forwarded, got %q", provider.taskAuthIndex)
	}
	body := resp.Body.String()
	if strings.Contains(body, `"taskId"`) || strings.Contains(body, `"cachedAt"`) || !strings.Contains(body, `"file_name":"claude-user.json"`) || !strings.Contains(body, `"refreshed_at":"2026-05-26T12:00:00Z"`) || !strings.Contains(body, `"status":"completed"`) || !strings.Contains(body, `"quota":{"id":"auth-1"`) || !strings.Contains(body, `"key":"rate_limit.primary_window"`) || !strings.Contains(body, `"subscription":{"provider":"codex","plan":"pro-20x"}`) || strings.Contains(body, `"planType"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshTaskMapsNotFoundTo404(t *testing.T) {
	provider := &quotaProviderStub{taskErr: quota.ErrTaskNotFound}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveAPIGet(router, "/api/v1/quota/refresh/missing-task")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestQuotaDoesNotExposeProviderSpecificEndpoints(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: &quotaProviderStub{}})
	paths := []string{
		"/api/v1/quota/antigravity",
		"/api/v1/quota/codex",
		"/api/v1/quota/gemini-cli",
		"/api/v1/quota/gemini-cli/code-assist",
		"/api/v1/quota/claude",
		"/api/v1/quota/kimi",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d", path, resp.Code)
		}
	}
}

func TestQuotaResetReturnsResetResponse(t *testing.T) {
	provider := &quotaProviderStub{resetResponse: quota.ResetResponse{AuthIndex: "codex-auth", Code: "reset", WindowsReset: 2}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"codex-auth"}`)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.resetRequest.AuthIndex != "codex-auth" {
		t.Fatalf("expected reset request auth_index codex-auth, got %+v", provider.resetRequest)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"authIndex":"codex-auth"`) || !strings.Contains(body, `"code":"reset"`) || !strings.Contains(body, `"windowsReset":2`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaResetRejectsEmptyAuthIndex(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"   "}`)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.resetRequest.AuthIndex != "" {
		t.Fatalf("provider should not be called for empty auth_index, got %+v", provider.resetRequest)
	}
}

func TestQuotaResetMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		status   int
		messages []string
	}{
		{name: "not found", err: quota.ErrNotFound, status: http.StatusNotFound},
		{name: "unsupported", err: quota.ErrUnsupportedType, status: http.StatusBadRequest, messages: []string{`"error":"quota_reset_failed"`, "quota identity type is unsupported"}},
		{name: "rate limited", err: quota.ProviderHTTPError{StatusCode: 429, Message: "rate limited"}, status: http.StatusTooManyRequests, messages: []string{`"error":"quota_reset_failed"`, "HTTP 429: rate limited"}},
		{name: "provider unauthorized", err: quota.ProviderHTTPError{StatusCode: 401, Message: "invalid codex token"}, status: http.StatusBadGateway, messages: []string{`"error":"quota_reset_failed"`, "HTTP 401: invalid codex token"}},
		{name: "validation", err: quota.ErrValidation, status: http.StatusBadRequest, messages: []string{"auth_index is required"}},
		{name: "in progress", err: quota.ErrResetInProgress, status: http.StatusConflict, messages: []string{`"error":"quota_reset_failed"`, "quota reset already in progress"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &quotaProviderStub{resetErr: tc.err}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
			resp := serveCredentialMutation(router, http.MethodPost, "/api/v1/quota/reset", `{"auth_index":"codex-auth"}`)
			if resp.Code != tc.status {
				t.Fatalf("status=%d, want %d body=%s", resp.Code, tc.status, resp.Body.String())
			}
			for _, message := range tc.messages {
				if !strings.Contains(resp.Body.String(), message) {
					t.Fatalf("missing %q: %s", message, resp.Body.String())
				}
			}
		})
	}
}
