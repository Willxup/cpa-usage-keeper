package test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
)

func TestUsageOverviewAloneAcceptsCustomDayRangeOlderThanThirtyDays(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -120)
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {startDay.Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	overviewResponse := httptest.NewRecorder()
	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?"+query.Encode(), nil)
	router.ServeHTTP(overviewResponse, overviewRequest)

	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("expected long custom Overview range to return 200, got %d body=%s", overviewResponse.Code, overviewResponse.Body.String())
	}
	if provider.overviewCalls != 1 {
		t.Fatalf("expected Overview provider to be called once, got %d", provider.overviewCalls)
	}
	if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startDay) {
		t.Fatalf("expected custom day start %s, got %+v", startDay, provider.lastFilter)
	}
	expectedEnd := today.AddDate(0, 0, 1)
	if provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(expectedEnd) || !provider.lastFilter.EndExclusive {
		t.Fatalf("expected exclusive custom day end %s, got %+v", expectedEnd, provider.lastFilter)
	}
	if provider.lastFilter.CustomUnit != "day" || provider.lastFilter.RangeCount != 121 {
		t.Fatalf("expected 121 complete custom day buckets, got %+v", provider.lastFilter)
	}

	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?"+query.Encode(), nil)
	router.ServeHTTP(eventsResponse, eventsRequest)

	if eventsResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected the same long custom Events range to remain rejected, got %d body=%s", eventsResponse.Code, eventsResponse.Body.String())
	}
	if provider.filterCalls != 0 {
		t.Fatalf("expected invalid Events range not to reach provider, got %d calls", provider.filterCalls)
	}
}

func TestKeyOverviewAcceptsCustomDayRangeOlderThanThirtyDays(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -120)
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {startDay.Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}
	provider := &usageEventsStub{}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer"}}
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?"+query.Encode(), nil)
	request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected long custom Key Overview range to return 200, got %d body=%s", response.Code, response.Body.String())
	}
	if provider.overviewCalls != 1 || provider.lastFilter.APIKeyID != "42" {
		t.Fatalf("expected Key Overview to query viewer key once, calls=%d filter=%+v", provider.overviewCalls, provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startDay) {
		t.Fatalf("expected custom day start %s, got %+v", startDay, provider.lastFilter)
	}
	expectedEnd := today.AddDate(0, 0, 1)
	if provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(expectedEnd) || !provider.lastFilter.EndExclusive {
		t.Fatalf("expected exclusive custom day end %s, got %+v", expectedEnd, provider.lastFilter)
	}
}
