package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/quota"
)

type quotaAutoRefreshSettingsProviderStub struct {
	QuotaProvider
	settings      quota.AutoRefreshSettings
	updateRequest quota.AutoRefreshSettings
	updateErr     error
}

func (s *quotaAutoRefreshSettingsProviderStub) GetAutoRefreshSettings(context.Context) (quota.AutoRefreshSettings, error) {
	return s.settings, nil
}

func (s *quotaAutoRefreshSettingsProviderStub) UpdateAutoRefreshSettings(_ context.Context, settings quota.AutoRefreshSettings) (quota.AutoRefreshSettings, error) {
	s.updateRequest = settings
	return settings, s.updateErr
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
	for _, tc := range []struct {
		name, body string
		want       *quota.AutoRefreshSchedule
	}{
		{"schedule", `{"enabled":true,"schedule":{"unit":"week","value":2}}`, &quota.AutoRefreshSchedule{Unit: quota.AutoRefreshScheduleUnitWeek, Value: 2}},
		{"enabled without schedule", `{"enabled":true,"schedule":null}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &quotaAutoRefreshSettingsProviderStub{}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/quota/auto-refresh/settings", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
			}
			if !provider.updateRequest.Enabled || !reflect.DeepEqual(provider.updateRequest.Schedule, tc.want) {
				t.Fatalf("update request=%+v, want enabled schedule=%+v", provider.updateRequest, tc.want)
			}
		})
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
