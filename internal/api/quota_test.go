package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/quota"
)

type quotaProviderStub struct {
	request         quota.CheckRequest
	response        quota.CheckResponse
	err             error
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

func (s *quotaProviderStub) Check(ctx context.Context, request quota.CheckRequest) (quota.CheckResponse, error) {
	s.request = request
	if s.err != nil {
		return quota.CheckResponse{}, s.err
	}
	return s.response, nil
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

func floatPtr(value float64) *float64 {
	return &value
}

func TestQuotaCheckReturnsProviderResponse(t *testing.T) {
	provider := &quotaProviderStub{response: quota.CheckResponse{
		ID: "codex-auth",
		Quota: []quota.QuotaRow{{
			Key:       "rate_limit.primary_window",
			Label:     "5h",
			Scope:     "window",
			Remaining: floatPtr(10),
		}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"codex-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.request.AuthIndex != "codex-auth" {
		t.Fatalf("expected auth_index to be forwarded, got %+v", provider.request)
	}
	body := resp.Body.String()
	if !contains(body, `"id":"codex-auth"`) || !contains(body, `"quota":[`) || !contains(body, `"remaining":10`) || contains(body, `"auth_index"`) || contains(body, `"provider"`) || contains(body, `"type"`) || contains(body, `"result"`) || contains(body, `"planType"`) || contains(body, `"name"`) || contains(body, `"identity"`) || contains(body, `"account_id"`) || contains(body, `"project_id"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaCheckReturnsProviderSpecificResultShape(t *testing.T) {
	provider := &quotaProviderStub{response: quota.CheckResponse{
		ID: "gemini-auth",
		Quota: []quota.QuotaRow{
			{Key: "bucket.gemini-2.5-pro_vertex.PROMPT", Label: "gemini-2.5-pro_vertex", Scope: "model", Metric: "PROMPT", RemainingFraction: floatPtr(0.7), Remaining: floatPtr(42), ResetAt: "2026-05-09T12:00:00Z"},
			{Key: "code_assist.current_tier.GOOGLE_ONE_AI", Label: "Code Assist Credit", Scope: "credits", Metric: "GOOGLE_ONE_AI", Remaining: floatPtr(10)},
		},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"gemini-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !contains(body, `"id":"gemini-auth"`) || !contains(body, `"bucket.gemini-2.5-pro_vertex.PROMPT"`) || !contains(body, `"code_assist.current_tier.GOOGLE_ONE_AI"`) || contains(body, `"quota_items"`) || contains(body, `"limits"`) || contains(body, `"auth_index"`) || contains(body, `"provider"`) || contains(body, `"type"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaCheckRejectsMissingAuthIndex(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.request.AuthIndex != "" {
		t.Fatalf("provider should not be called for missing auth_index, got %+v", provider.request)
	}
}

func TestQuotaCheckMapsNotFoundTo404(t *testing.T) {
	provider := &quotaProviderStub{err: quota.ErrNotFound}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"missing-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestQuotaCheckMapsUnsupportedTypeTo422(t *testing.T) {
	provider := &quotaProviderStub{err: quota.ErrUnsupportedType}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"unknown-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestQuotaCheckMapsProviderInputTo422(t *testing.T) {
	provider := &quotaProviderStub{err: errors.Join(quota.ErrProviderInput, errors.New("missing project_id parameter"))}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"gemini-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !contains(resp.Body.String(), "missing project_id parameter") {
		t.Fatalf("expected provider input message in response, got %s", resp.Body.String())
	}
}

func TestQuotaCacheReturnsCachedCurrentPageQuota(t *testing.T) {
	provider := &quotaProviderStub{cacheResponse: quota.CacheResponse{
		Items: []quota.CheckResponse{{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.secondary_window", Label: "Weekly"}}}},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/cache", strings.NewReader(`{"auth_indexes":["auth-1","auth-2"],"limit":20}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := strings.Join(provider.cacheRequest.AuthIndexes, ","); got != "auth-1,auth-2" {
		t.Fatalf("expected auth indexes to be forwarded, got %+v", provider.cacheRequest.AuthIndexes)
	}
	if provider.cacheRequest.Limit != 20 {
		t.Fatalf("expected outer cache limit 20, got %d", provider.cacheRequest.Limit)
	}
	body := resp.Body.String()
	if !contains(body, `"items"`) || !contains(body, `"id":"auth-1"`) || !contains(body, `"label":"Weekly"`) {
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
	bodyBytes, err := json.Marshal(map[string]any{"auth_indexes": authIndexes, "limit": 50})
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
	if provider.cacheRequest.Limit != 50 || len(provider.cacheRequest.AuthIndexes) != 21 {
		t.Fatalf("expected cache request to bypass refresh limit, got %+v", provider.cacheRequest)
	}
}

func TestQuotaRefreshCreatesTasksForCurrentPageAuthIndexes(t *testing.T) {
	provider := &quotaProviderStub{refreshResponse: quota.RefreshResponse{
		Tasks:    []quota.RefreshTaskID{{AuthIndex: "auth-1", TaskID: "task-1"}, {AuthIndex: "auth-2", TaskID: "task-2"}},
		Accepted: 2,
		Limit:    20,
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":["auth-1","auth-2"],"limit":20}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := strings.Join(provider.refreshRequest.AuthIndexes, ","); got != "auth-1,auth-2" {
		t.Fatalf("expected auth indexes to be forwarded, got %+v", provider.refreshRequest.AuthIndexes)
	}
	if provider.refreshRequest.Limit != 20 {
		t.Fatalf("expected outer refresh limit 20, got %d", provider.refreshRequest.Limit)
	}
	if provider.refreshRequest.Source != quota.RefreshSourceManual {
		t.Fatalf("expected manual refresh source, got %q", provider.refreshRequest.Source)
	}
	body := resp.Body.String()
	if !contains(body, `"tasks"`) || !contains(body, `"taskId":"task-1"`) || !contains(body, `"accepted":2`) || !contains(body, `"limit":20`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaRefreshRejectsTooManyAuthIndexesAtOuterLayer(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	authIndexes := make([]string, 0, 21)
	for i := 0; i < 21; i++ {
		authIndexes = append(authIndexes, `"auth"`)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":[`+strings.Join(authIndexes, ",")+"]}"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.refreshRequest.AuthIndexes != nil {
		t.Fatalf("provider should not be called for oversized refresh request, got %+v", provider.refreshRequest)
	}
}

func TestQuotaRefreshRejectsEmptyAuthIndexes(t *testing.T) {
	provider := &quotaProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/refresh", strings.NewReader(`{"auth_indexes":[],"limit":20}`))
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
		Quota:     &quota.CheckResponse{ID: "auth-1", Quota: []quota.QuotaRow{{Key: "rate_limit.primary_window", Label: "5h"}}},
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
	if !contains(body, `"status":"completed"`) || !contains(body, `"quota":{"id":"auth-1"`) || !contains(body, `"key":"rate_limit.primary_window"`) {
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

func TestQuotaCheckMapsWrappedErrors(t *testing.T) {
	provider := &quotaProviderStub{err: errors.Join(quota.ErrNotFound, errors.New("missing"))}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/quota/check", strings.NewReader(`{"auth_index":"missing-auth"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}
