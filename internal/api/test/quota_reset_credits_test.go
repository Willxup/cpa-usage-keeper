package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/quota"
)

type quotaResetCreditsProviderStub struct {
	request  quota.ResetCreditsRequest
	response quota.ResetCreditsResponse
	err      error
}

func (s *quotaResetCreditsProviderStub) GetCachedQuota(context.Context, quota.CacheRequest) (quota.CacheResponse, error) {
	return quota.CacheResponse{}, nil
}

func (s *quotaResetCreditsProviderStub) Refresh(context.Context, quota.RefreshRequest) (quota.RefreshResponse, error) {
	return quota.RefreshResponse{}, nil
}

func (s *quotaResetCreditsProviderStub) GetRefreshTaskByAuthIndex(context.Context, string) (quota.RefreshTaskResponse, error) {
	return quota.RefreshTaskResponse{}, nil
}

func (s *quotaResetCreditsProviderStub) GetInspectionStatus(context.Context) (quota.InspectionStatus, error) {
	return quota.InspectionStatus{}, nil
}

func (s *quotaResetCreditsProviderStub) StartInspection(context.Context) (quota.InspectionStatus, error) {
	return quota.InspectionStatus{}, nil
}

func (s *quotaResetCreditsProviderStub) GetAutoRefreshSettings(context.Context) (quota.AutoRefreshSettings, error) {
	return quota.AutoRefreshSettings{}, nil
}

func (s *quotaResetCreditsProviderStub) UpdateAutoRefreshSettings(_ context.Context, settings quota.AutoRefreshSettings) (quota.AutoRefreshSettings, error) {
	return settings, nil
}

func (s *quotaResetCreditsProviderStub) GetResetCredits(_ context.Context, request quota.ResetCreditsRequest) (quota.ResetCreditsResponse, error) {
	s.request = request
	if s.err != nil {
		return quota.ResetCreditsResponse{}, s.err
	}
	return s.response, nil
}

func (s *quotaResetCreditsProviderStub) Reset(context.Context, quota.ResetRequest) (quota.ResetResponse, error) {
	return quota.ResetResponse{}, nil
}

func TestQuotaResetCreditsReturnsAvailableExpiries(t *testing.T) {
	availableCount := 2
	provider := &quotaResetCreditsProviderStub{response: quota.ResetCreditsResponse{
		AuthIndex:      "codex-auth",
		AvailableCount: &availableCount,
		Credits: []quota.CodexRateLimitResetCredit{
			{ID: "credit-1", Status: "available", GrantedAt: "2026-07-01T00:00:00Z", ExpiresAt: "2026-07-20T00:00:00Z"},
			{ID: "credit-2", Status: "available", GrantedAt: "2026-07-02T00:00:00Z", ExpiresAt: "2026-07-21T00:00:00Z"},
		},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/reset-credits/codex-auth", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.request.AuthIndex != "codex-auth" {
		t.Fatalf("expected reset credit request auth_index codex-auth, got %+v", provider.request)
	}
	body := resp.Body.String()
	if !contains(body, `"authIndex":"codex-auth"`) || !contains(body, `"availableCount":2`) || !contains(body, `"expiresAt":"2026-07-20T00:00:00Z"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestQuotaResetCreditsReturnsNullWhenAvailableCountIsUnknown(t *testing.T) {
	provider := &quotaResetCreditsProviderStub{response: quota.ResetCreditsResponse{
		AuthIndex: "codex-auth",
		Credits:   []quota.CodexRateLimitResetCredit{},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/reset-credits/codex-auth", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !contains(body, `"availableCount":null`) {
		t.Fatalf("expected unknown available count to remain null, got %s", body)
	}
}

func TestQuotaResetCreditsMapsProviderUnauthorizedAwayFromAppAuth(t *testing.T) {
	provider := &quotaResetCreditsProviderStub{err: quota.ProviderHTTPError{StatusCode: http.StatusUnauthorized, Message: "invalid codex token"}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/reset-credits/codex-auth", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected provider 401 to map to 502, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := resp.Body.String(); !contains(body, `"error":"quota_reset_credits_failed"`) || !contains(body, "HTTP 401: invalid codex token") {
		t.Fatalf("unexpected response body: %s", body)
	}
}
