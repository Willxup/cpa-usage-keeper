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

func TestUsageRoutesAcceptCustomHourSlots(t *testing.T) {
	currentHour := time.Now().In(time.Local).Truncate(time.Hour)
	startHour := currentHour.Add(-4 * time.Hour)
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	query := url.Values{
		"range": {"custom"},
		"unit":  {"hour"},
		"start": {startHour.Format(time.RFC3339)},
		"end":   {currentHour.Format(time.RFC3339)},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?"+query.Encode(), nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected custom hour range to return 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startHour) {
		t.Fatalf("expected custom hour start %s, got %+v", startHour, provider.lastFilter)
	}
	expectedEnd := currentHour.Add(time.Hour)
	if provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(expectedEnd) || !provider.lastFilter.EndExclusive {
		t.Fatalf("expected exclusive custom hour end %s, got %+v", expectedEnd, provider.lastFilter)
	}
	if provider.lastFilter.CustomUnit != "hour" {
		t.Fatalf("expected custom hour unit, got %+v", provider.lastFilter)
	}
}

func TestUsageRoutesAcceptCustomDaySlots(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -29)
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {startDay.Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?"+query.Encode(), nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected custom day range to return 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startDay) {
		t.Fatalf("expected custom day start %s, got %+v", startDay, provider.lastFilter)
	}
	expectedEnd := today.AddDate(0, 0, 1)
	if provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(expectedEnd) || !provider.lastFilter.EndExclusive {
		t.Fatalf("expected exclusive custom day end %s, got %+v", expectedEnd, provider.lastFilter)
	}
	if provider.lastFilter.CustomUnit != "day" {
		t.Fatalf("expected custom day unit, got %+v", provider.lastFilter)
	}
}

func TestUsageRoutesRejectCustomRangesOutsideProductBounds(t *testing.T) {
	now := time.Now().In(time.Local)
	currentHour := now.Truncate(time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	testCases := []struct {
		name       string
		unit       string
		start      string
		end        string
		wantStatus int
	}{
		{name: "four hour slots", unit: "hour", start: currentHour.Add(-3 * time.Hour).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "hour before horizon", unit: "hour", start: currentHour.Add(-24 * time.Hour).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "future hour", unit: "hour", start: currentHour.Add(-3 * time.Hour).Format(time.RFC3339), end: currentHour.Add(time.Hour).Format(time.RFC3339), wantStatus: http.StatusConflict},
		{name: "unaligned hour", unit: "hour", start: currentHour.Add(-4*time.Hour + time.Minute).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "366 day slots", unit: "day", start: today.AddDate(0, 0, -365).Format(time.DateOnly), end: today.Format(time.DateOnly), wantStatus: http.StatusBadRequest},
		{name: "future day", unit: "day", start: today.Format(time.DateOnly), end: today.AddDate(0, 0, 1).Format(time.DateOnly), wantStatus: http.StatusConflict},
		{name: "mixed day and hour", unit: "day", start: today.Format(time.DateOnly), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			query := url.Values{"range": {"custom"}, "unit": {tc.unit}, "start": {tc.start}, "end": {tc.end}}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?"+query.Encode(), nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != tc.wantStatus {
				t.Fatalf("expected invalid custom range to return %d, got %d body=%s", tc.wantStatus, resp.Code, resp.Body.String())
			}
			if provider.filterCalls != 0 {
				t.Fatalf("expected invalid custom range not to reach provider, got %d calls", provider.filterCalls)
			}
		})
	}
}

func TestKeyOverviewAcceptsCustomRange(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	provider := &usageEventsStub{}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer"}}
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {today.AddDate(0, 0, -6).Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?"+query.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected key overview custom range to return 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.CustomUnit != "day" || !provider.lastFilter.EndExclusive || provider.lastFilter.APIKeyID != "42" {
		t.Fatalf("expected key overview custom filter to keep range and force viewer key, got %+v", provider.lastFilter)
	}
}
