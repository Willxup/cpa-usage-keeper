package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/quota"
)

type quotaAutoRefreshSettingsProviderStub struct {
	settings      quota.AutoRefreshSettings
	settingsErr   error
	updateRequest quota.AutoRefreshSettings
	updateErr     error
}

func (s *quotaAutoRefreshSettingsProviderStub) Refresh(context.Context, quota.RefreshRequest) (quota.RefreshResponse, error) {
	return quota.RefreshResponse{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) GetRefreshTaskByAuthIndex(context.Context, string) (quota.RefreshTaskResponse, error) {
	return quota.RefreshTaskResponse{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) GetCachedQuota(context.Context, quota.CacheRequest) (quota.CacheResponse, error) {
	return quota.CacheResponse{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) GetInspectionStatus(context.Context) (quota.InspectionStatus, error) {
	return quota.InspectionStatus{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) StartInspection(context.Context) (quota.InspectionStatus, error) {
	return quota.InspectionStatus{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) GetAutoRefreshSettings(context.Context) (quota.AutoRefreshSettings, error) {
	if s.settingsErr != nil {
		return quota.AutoRefreshSettings{}, s.settingsErr
	}
	return s.settings, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) UpdateAutoRefreshSettings(_ context.Context, settings quota.AutoRefreshSettings) (quota.AutoRefreshSettings, error) {
	s.updateRequest = settings
	if s.updateErr != nil {
		return quota.AutoRefreshSettings{}, s.updateErr
	}
	return settings, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) Reset(context.Context, quota.ResetRequest) (quota.ResetResponse, error) {
	return quota.ResetResponse{}, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) GetResetCredits(context.Context, quota.ResetCreditsRequest) (quota.ResetCreditsResponse, error) {
	return quota.ResetCreditsResponse{}, nil
}

func TestQuotaAutoRefreshSettingsReturnsTypedSchedule(t *testing.T) {
	provider := &quotaAutoRefreshSettingsProviderStub{settings: quota.AutoRefreshSettings{
		Enabled: true,
		Schedule: &quota.AutoRefreshSchedule{
			Unit:  quota.AutoRefreshScheduleUnitHour,
			Value: 6,
		},
	}}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quota/auto-refresh/settings", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !contains(body, `"enabled":true`) || !contains(body, `"unit":"hour"`) || !contains(body, `"value":6`) {
		t.Fatalf("unexpected settings response: %s", body)
	}
}

func TestQuotaAutoRefreshSettingsUpdatesTypedSchedule(t *testing.T) {
	provider := &quotaAutoRefreshSettingsProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quota/auto-refresh/settings", strings.NewReader(`{"enabled":true,"schedule":{"unit":"week","value":2}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !provider.updateRequest.Enabled || provider.updateRequest.Schedule == nil || provider.updateRequest.Schedule.Unit != quota.AutoRefreshScheduleUnitWeek || provider.updateRequest.Schedule.Value != 2 {
		t.Fatalf("unexpected update request: %+v", provider.updateRequest)
	}
}

func TestQuotaAutoRefreshSettingsAllowsEnabledWithoutSchedule(t *testing.T) {
	provider := &quotaAutoRefreshSettingsProviderStub{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quota/auto-refresh/settings", strings.NewReader(`{"enabled":true,"schedule":null}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !provider.updateRequest.Enabled || provider.updateRequest.Schedule != nil {
		t.Fatalf("unexpected update request: %+v", provider.updateRequest)
	}
}

func TestQuotaAutoRefreshSettingsRejectsInvalidSchedule(t *testing.T) {
	provider := &quotaAutoRefreshSettingsProviderStub{updateErr: quota.ErrValidation}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/quota/auto-refresh/settings", strings.NewReader(`{"enabled":true,"schedule":{"unit":"minute","value":61}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}
