package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

type quotaProviderStub struct {
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

func (s *quotaProviderStub) Refresh(ctx context.Context, request quota.RefreshRequest) (quota.RefreshResponse, error) {
	s.refreshRequest = request
	if s.refreshErr != nil {
		return quota.RefreshResponse{}, s.refreshErr
	}
	return s.refreshResponse, nil
}

func (s *quotaProviderStub) GetRefreshTaskByAuthIndex(ctx context.Context, authIndex string) (quota.RefreshTaskResponse, error) {
	s.taskAuthIndex = authIndex
	if s.taskErr != nil {
		return quota.RefreshTaskResponse{}, s.taskErr
	}
	return s.taskResponse, nil
}

func (s *quotaProviderStub) GetCachedQuota(ctx context.Context, request quota.CacheRequest) (quota.CacheResponse, error) {
	s.cacheRequest = request
	if s.cacheErr != nil {
		return quota.CacheResponse{}, s.cacheErr
	}
	return s.cacheResponse, nil
}

func (s *quotaProviderStub) GetInspectionStatus(ctx context.Context) (quota.InspectionStatus, error) {
	s.inspectionStatusCalls++
	if s.inspectionStatusErr != nil {
		return quota.InspectionStatus{}, s.inspectionStatusErr
	}
	return s.inspectionStatusResponse, nil
}

func (s *quotaProviderStub) StartInspection(ctx context.Context) (quota.InspectionStatus, error) {
	s.inspectionStartCalls++
	if s.inspectionStartErr != nil {
		return quota.InspectionStatus{}, s.inspectionStartErr
	}
	return s.inspectionStartResponse, nil
}

func TestQuotaCacheReturnsCachedCurrentPageQuota(t *testing.T) {
	refreshedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{
		Items: []quota.CachedQuotaItem{{AuthIndex: "auth-1", FileName: apiStringPtr("claude-user.json"), Status: quota.RefreshTaskStatusCompleted, RefreshedAt: &refreshedAt, Quota: &quota.CheckResponse{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.secondary_window", Label: "Weekly", PlanType: "plus"}}}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/cache", strings.NewReader(`{"auth_indexes":["auth-1","auth-2"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := strings.Join(provider.cacheRequest.AuthIndexes, ","); got != "auth-1,auth-2" {
		t.Fatalf("expected auth indexes to be forwarded, got %+v", provider.cacheRequest.AuthIndexes)
	}
	body := resp.Body.String()
	if !contains(body, `"items"`) || !contains(body, `"file_name":"claude-user.json"`) || !contains(body, `"refreshed_at":"2026-05-26T12:00:00Z"`) || contains(body, `"updated_at"`) || !contains(body, `"id":"auth-1"`) || !contains(body, `"label":"Weekly"`) || !contains(body, `"planType":"plus"`) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/cache", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

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
		Total: 3, Cached: 2, Running: true, Normal: 1, Unauthorized401: 1, CompletedAt: &completedAt,
		Results: []quota.InspectionResult{{AuthIndex: "auth-1", Name: "Claude Main", Type: "claude", FileName: apiStringPtr("claude-user.json"), Status: quota.InspectionResultStatusNormal, RefreshedAt: &refreshedAt}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/inspection", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.inspectionStatusCalls != 1 || provider.inspectionStartCalls != 0 {
		t.Fatalf("expected status lookup only, got status=%d start=%d", provider.inspectionStatusCalls, provider.inspectionStartCalls)
	}
	body := resp.Body.String()
	if !contains(body, `"total":3`) || !contains(body, `"cached":2`) || !contains(body, `"unauthorized_401":1`) || !contains(body, `"completed_at":"2026-06-03T10:31:00Z"`) || !contains(body, `"auth_index":"auth-1"`) || !contains(body, `"file_name":"claude-user.json"`) || !contains(body, `"refreshed_at":"2026-06-03T10:30:00Z"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if contains(body, `"provider"`) {
		t.Fatalf("expected inspection response to use type/name only, got %s", body)
	}
}

func TestQuotaInspectionStartReturnsFreshStatus(t *testing.T) {
	provider := &quotaProviderStub{inspectionStartResponse: quota.InspectionStatus{Total: 2, Cached: 0, Running: true}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/inspection", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.inspectionStartCalls != 1 || provider.inspectionStatusCalls != 0 {
		t.Fatalf("expected inspection start only, got start=%d status=%d", provider.inspectionStartCalls, provider.inspectionStatusCalls)
	}
	if body := resp.Body.String(); !contains(body, `"total":2`) || !contains(body, `"cached":0`) || !contains(body, `"running":true`) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":["auth-1","auth-2"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

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
	if !contains(body, `"tasks"`) || !contains(body, `"authIndex":"auth-1"`) || contains(body, `"taskId"`) || !contains(body, `"accepted":2`) || !contains(body, `"limit":2`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshAllowsCurrentPageSizeWithoutOuterTwentyLimit(t *testing.T) {
	provider := &quotaProviderStub{refreshResponse: quota.RefreshResponse{Accepted: 25, Limit: 25}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	authIndexes := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		authIndexes = append(authIndexes, `"auth"`)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":[`+strings.Join(authIndexes, ",")+"]}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

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
		FileName:    apiStringPtr("claude-user.json"),
		Status:      quota.RefreshTaskStatusCompleted,
		RefreshedAt: &refreshedAt,
		Quota:       &quota.CheckResponse{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", PlanType: "pro"}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/refresh/auth-1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.taskAuthIndex != "auth-1" {
		t.Fatalf("expected auth_index to be forwarded, got %q", provider.taskAuthIndex)
	}
	body := resp.Body.String()
	if contains(body, `"taskId"`) || contains(body, `"cachedAt"`) || !contains(body, `"file_name":"claude-user.json"`) || !contains(body, `"refreshed_at":"2026-05-26T12:00:00Z"`) || !contains(body, `"status":"completed"`) || !contains(body, `"quota":{"id":"auth-1"`) || !contains(body, `"key":"rate_limit.primary_window"`) || !contains(body, `"planType":"pro"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshTaskMapsNotFoundTo404(t *testing.T) {
	provider := &quotaProviderStub{taskErr: quota.ErrTaskNotFound}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/refresh/missing-task", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

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
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d", path, resp.Code)
		}
	}
}
