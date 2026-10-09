package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/quota"
)

type quotaHistoryDeleteProvider struct {
	quotaAutoRefreshSettingsProviderStub
	authIndex string
	cycleID   int64
	err       error
}

func (p *quotaHistoryDeleteProvider) DeleteCodexQuotaHistoryCycle(_ context.Context, authIndex string, cycleID int64) error {
	p.authIndex, p.cycleID = authIndex, cycleID
	return p.err
}

func TestQuotaHistoryDeleteRoute(t *testing.T) {
	for _, testCase := range []struct {
		name, id string
		err      error
		status   int
	}{
		{"success", "42", nil, 204}, {"missing", "42", quota.ErrNotFound, 404},
		{"unsupported", "42", quota.ErrUnsupportedType, 400}, {"database error", "42", errors.New("failed"), 500},
		{"negative id", "-1", nil, 400}, {"zero id", "0", nil, 400}, {"invalid id", "bad", nil, 400},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &quotaHistoryDeleteProvider{err: testCase.err}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{Quota: provider})
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/quota/history/test-auth/cycles/"+testCase.id, nil)
			request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if testCase.id == "42" && (provider.authIndex != "test-auth" || provider.cycleID != 42) {
				t.Fatalf("request not forwarded: %+v", provider)
			}
			if testCase.id != "42" && provider.cycleID != 0 {
				t.Fatal("invalid ID reached provider")
			}
		})
	}
}

func TestQuotaHistoryDeleteRequiresAdminSession(t *testing.T) {
	provider := &quotaHistoryDeleteProvider{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{Enabled: true, LoginPassword: "test-password", SessionTTL: time.Hour}, nil, "", OptionalProviders{Quota: provider})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/quota/history/test-auth/cycles/42", nil)
	request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || provider.cycleID != 0 {
		t.Fatalf("unauthenticated delete: %d", response.Code)
	}
}
