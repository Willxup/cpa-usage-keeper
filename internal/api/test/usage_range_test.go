package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
)

func TestUsageRoutesAcceptBoundedRollingRanges(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rangeVal string
		duration time.Duration
	}{
		{name: "minimum hours", rangeVal: "5h", duration: 5 * time.Hour},
		{name: "arbitrary hours", rangeVal: "13h", duration: 13 * time.Hour},
		{name: "maximum hours", rangeVal: "24h", duration: 24 * time.Hour},
		{name: "one day", rangeVal: "1d", duration: 24 * time.Hour},
		{name: "arbitrary days", rangeVal: "17d", duration: 17 * 24 * time.Hour},
		{name: "maximum days", rangeVal: "30d", duration: 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range="+tc.rangeVal, nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("expected rolling range %q to return 200, got %d body=%s", tc.rangeVal, resp.Code, resp.Body.String())
			}
			if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
				t.Fatalf("expected concrete rolling bounds, got %+v", provider.lastFilter)
			}
			if got := provider.lastFilter.EndTime.Sub(*provider.lastFilter.StartTime); got != tc.duration {
				t.Fatalf("expected %q duration %s, got %s", tc.rangeVal, tc.duration, got)
			}
		})
	}
}

func TestUsageRoutesRejectOutOfBoundsRollingRanges(t *testing.T) {
	for _, rangeVal := range []string{"0h", "1h", "4h", "25h", "0d", "31d", "01h", "1w"} {
		t.Run(rangeVal, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range="+rangeVal, nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected rolling range %q to return 400, got %d body=%s", rangeVal, resp.Code, resp.Body.String())
			}
			if provider.filterCalls != 0 {
				t.Fatalf("expected invalid range not to reach provider, got %d calls", provider.filterCalls)
			}
		})
	}
}

func TestKeyOverviewAcceptsBoundedRollingRange(t *testing.T) {
	provider := &usageEventsStub{}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer"}}
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=13d", nil)
	req.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected key overview rolling range to return 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}
