package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/quota"
)

type quotaProviderStub struct {
	refreshRequest  quota.RefreshRequest
	refreshResponse quota.RefreshResponse
	refreshErr      error
	taskID          string
	taskResponse    quota.RefreshTaskResponse
	taskErr         error
	cacheRequest    quota.CacheRequest
	cacheResponse   quota.CacheResponse
	cacheErr        error
}

func (s *quotaProviderStub) Refresh(ctx context.Context, request quota.RefreshRequest) (quota.RefreshResponse, error) {
	s.refreshRequest = request
	if s.refreshErr != nil {
		return quota.RefreshResponse{}, s.refreshErr
	}
	return s.refreshResponse, nil
}

func (s *quotaProviderStub) GetRefreshTask(ctx context.Context, taskID string) (quota.RefreshTaskResponse, error) {
	s.taskID = taskID
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

func TestQuotaCacheReturnsCachedCurrentPageQuota(t *testing.T) {
	provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{
		Items: []quota.CheckResponse{{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.secondary_window", Label: "Weekly", PlanType: "plus"}}}},
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
	if !contains(body, `"items"`) || !contains(body, `"id":"auth-1"`) || !contains(body, `"label":"Weekly"`) || !contains(body, `"planType":"plus"`) {
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

func TestQuotaRefreshCreatesTasksForCurrentPageAuthIndexes(t *testing.T) {
	provider := &quotaProviderStub{refreshResponse: quota.RefreshResponse{
		Tasks:    []quota.RefreshTaskID{{AuthIndex: "auth-1", TaskID: "task-1"}, {AuthIndex: "auth-2", TaskID: "task-2"}},
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
	if !contains(body, `"tasks"`) || !contains(body, `"taskId":"task-1"`) || !contains(body, `"accepted":2`) || !contains(body, `"limit":2`) {
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

func TestQuotaRefreshTaskReturnsCachedQuota(t *testing.T) {
	provider := &quotaProviderStub{taskResponse: quota.RefreshTaskResponse{
		TaskID:    "task-1",
		AuthIndex: "auth-1",
		Status:    quota.RefreshTaskStatusCompleted,
		Quota:     &quota.CheckResponse{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.primary_window", Label: "5h", PlanType: "pro"}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/refresh/task-1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.taskID != "task-1" {
		t.Fatalf("expected task id to be forwarded, got %q", provider.taskID)
	}
	body := resp.Body.String()
	if !contains(body, `"status":"completed"`) || !contains(body, `"quota":{"id":"auth-1"`) || !contains(body, `"key":"rate_limit.primary_window"`) || !contains(body, `"planType":"pro"`) {
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
