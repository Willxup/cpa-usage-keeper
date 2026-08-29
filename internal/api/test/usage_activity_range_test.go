package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageActivityRouteStub struct {
	service.UsageProvider
	activity   *servicedto.UsageActivitySnapshot
	lastFilter servicedto.UsageFilter
	calls      int
}

func (s *usageActivityRouteStub) GetUsageActivity(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error) {
	s.calls++
	s.lastFilter = filter
	return s.activity, nil
}

func TestUsageActivityUsesOverviewTimeQueryAndAcceptsOptionalAPIKey(t *testing.T) {
	provider := &usageActivityRouteStub{activity: &servicedto.UsageActivitySnapshot{
		Window: servicedto.UsageActivityWindowWeek, Grain: "medium", Rows: 7, Columns: 52, Blocks: []servicedto.UsageActivityBlock{},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	for _, path := range []string{
		"/api/v1/usage/activity",
		"/api/v1/usage/activity?range=daily",
		"/api/v1/usage/activity?range=custom&unit=day",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("path %q status=%d, want 400: %s", path, response.Code, response.Body.String())
		}
	}
	if provider.calls != 0 {
		t.Fatalf("invalid Activity ranges called service %d times", provider.calls)
	}

	response := httptest.NewRecorder()
	path := "/api/v1/usage/activity?range=2d&page=0&page_size=25&result=bogus&api_key_id=42"
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("Admin Activity status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.calls != 1 || provider.lastFilter.Range != "2d" || provider.lastFilter.RangeUnit != "day" || provider.lastFilter.RangeCount != 2 || provider.lastFilter.APIKeyID != "42" {
		t.Fatalf("unexpected Admin Activity filter: calls=%d filter=%+v", provider.calls, provider.lastFilter)
	}
	if provider.lastFilter.Page != 0 || provider.lastFilter.Result != "" {
		t.Fatalf("Activity should not parse Events-only fields: %+v", provider.lastFilter)
	}

	oneYearResponse := httptest.NewRecorder()
	oneYearPath := "/api/v1/usage/activity?window=year&api_key_id=42"
	router.ServeHTTP(oneYearResponse, httptest.NewRequest(http.MethodGet, oneYearPath, nil))
	if oneYearResponse.Code != http.StatusOK {
		t.Fatalf("Admin one-year Activity status=%d body=%s", oneYearResponse.Code, oneYearResponse.Body.String())
	}
	if provider.calls != 2 || provider.lastFilter.ActivityWindow != servicedto.UsageActivityWindowYear || provider.lastFilter.APIKeyID != "42" {
		t.Fatalf("unexpected Admin one-year Activity filter: calls=%d filter=%+v", provider.calls, provider.lastFilter)
	}
}

func TestUsageActivityRejectsUnknownWindowBeforeCallingService(t *testing.T) {
	provider := &usageActivityRouteStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/activity?window=unknown", nil))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown Activity window status=%d, want 400: %s", response.Code, response.Body.String())
	}
	if provider.calls != 0 {
		t.Fatalf("unknown Activity window called service %d times, want 0", provider.calls)
	}
}

func TestUsageActivityAcceptsLongCustomDayRange(t *testing.T) {
	provider := &usageActivityRouteStub{activity: &servicedto.UsageActivitySnapshot{
		Window: servicedto.UsageActivityWindowYear, Grain: "daily", Rows: 7, Columns: 52, Blocks: []servicedto.UsageActivityBlock{},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -120)
	path := "/api/v1/usage/activity?range=custom&unit=day&start=" + startDay.Format(time.DateOnly) + "&end=" + today.Format(time.DateOnly) + "&api_key_id=42"

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

	if response.Code != http.StatusOK {
		t.Fatalf("long Custom day Activity status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.calls != 1 {
		t.Fatalf("long Custom day Activity provider calls=%d, want 1", provider.calls)
	}
	filter := provider.lastFilter
	if filter.Range != "custom" || filter.CustomUnit != "day" || filter.RangeUnit != "day" || filter.RangeCount != 121 || filter.APIKeyID != "42" {
		t.Fatalf("unexpected long Custom day Activity filter: %+v", filter)
	}
	if filter.QueryNow == nil || filter.StartTime == nil || !filter.StartTime.Equal(startDay) {
		t.Fatalf("long Custom day Activity did not preserve query time and start: %+v", filter)
	}
	expectedEnd := today.AddDate(0, 0, 1)
	if filter.EndTime == nil || !filter.EndTime.Equal(expectedEnd) || !filter.EndExclusive {
		t.Fatalf("long Custom day Activity end=%v exclusive=%v, want %s exclusive", filter.EndTime, filter.EndExclusive, expectedEnd)
	}
}

func TestUsageActivityAcceptsCalendarDayWindowModes(t *testing.T) {
	provider := &usageActivityRouteStub{activity: &servicedto.UsageActivitySnapshot{
		Window: servicedto.UsageActivityWindowDay, Grain: "short", Rows: 7, Columns: 52, Blocks: []servicedto.UsageActivityBlock{},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	for _, window := range []string{"today", "yesterday"} {
		t.Run(window, func(t *testing.T) {
			response := httptest.NewRecorder()
			path := "/api/v1/usage/activity?window=" + window + "&api_key_id=42"
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("Activity %s status=%d body=%s", window, response.Code, response.Body.String())
			}
			if provider.lastFilter.ActivityWindow != servicedto.UsageActivityWindow(window) || provider.lastFilter.Range != window {
				t.Fatalf("unexpected %s Activity filter: %+v", window, provider.lastFilter)
			}
			if provider.lastFilter.RangeUnit != "day" || provider.lastFilter.RangeCount != 1 || provider.lastFilter.APIKeyID != "42" {
				t.Fatalf("unexpected %s Activity identity: %+v", window, provider.lastFilter)
			}
			if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
				t.Fatalf("%s Activity did not preserve calendar bounds: %+v", window, provider.lastFilter)
			}
			start := provider.lastFilter.StartTime.In(time.Local)
			end := provider.lastFilter.EndTime.In(time.Local)
			if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || !end.Add(time.Nanosecond).Equal(start.AddDate(0, 0, 1)) {
				t.Fatalf("unexpected %s Activity bounds: %s..%s", window, start, end)
			}
		})
	}
}

func TestUsageActivityRangesReturnBackendSelectedFixedTierWindows(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-activity-ranges.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("resolve sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	router := NewRouter(nil, nil, service.NewUsageService(db, emptyPricingCatalogForTest()), nil, AuthConfig{}, nil, "")
	testCases := []struct {
		name         string
		query        string
		wantWindow   string
		wantDuration time.Duration
		wantDays     int
		wantCalendar bool
	}{
		{name: "hours", query: "range=8h", wantWindow: "day", wantDuration: 24 * time.Hour},
		{name: "explicit day", query: "window=day", wantWindow: "day", wantDuration: 24 * time.Hour},
		{name: "today", query: "range=today", wantWindow: "day", wantDuration: 24 * time.Hour, wantCalendar: true},
		{name: "yesterday", query: "range=yesterday", wantWindow: "day", wantDuration: 24 * time.Hour, wantCalendar: true},
		{name: "one day", query: "range=1d", wantWindow: "day", wantDuration: 24 * time.Hour},
		{name: "two days", query: "range=2d", wantWindow: "week", wantDuration: 7 * 24 * time.Hour},
		{name: "explicit week", query: "window=week", wantWindow: "week", wantDuration: 7 * 24 * time.Hour},
		{name: "seven days", query: "range=7d", wantWindow: "week", wantDuration: 7 * 24 * time.Hour},
		{name: "eight days", query: "range=8d", wantWindow: "month", wantDuration: 30 * 24 * time.Hour},
		{name: "explicit month", query: "window=month", wantWindow: "month", wantDuration: 30 * 24 * time.Hour},
		{name: "thirty days", query: "range=30d", wantWindow: "month", wantDuration: 30 * 24 * time.Hour},
		{name: "explicit year", query: "window=year", wantWindow: "year", wantDays: repository.UsageActivityHeatmapBlocks},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/activity?"+testCase.query, nil)
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", response.Code, response.Body.String())
			}
			var payload struct {
				Window      string    `json:"window"`
				Grain       string    `json:"grain"`
				Timezone    string    `json:"timezone"`
				Rows        int       `json:"rows"`
				Columns     int       `json:"columns"`
				WindowStart time.Time `json:"window_start"`
				WindowEnd   time.Time `json:"window_end"`
				Blocks      []struct {
					StartTime time.Time `json:"start_time"`
					EndTime   time.Time `json:"end_time"`
					Rate      float64   `json:"rate"`
				} `json:"blocks"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode Activity response: %v", err)
			}
			if payload.Window != testCase.wantWindow {
				t.Fatalf("Activity window=%q, want %q", payload.Window, testCase.wantWindow)
			}
			if payload.Rows != 7 || payload.Columns != 52 || len(payload.Blocks) != repository.UsageActivityHeatmapBlocks {
				t.Fatalf("unexpected Activity shape: rows=%d columns=%d blocks=%d", payload.Rows, payload.Columns, len(payload.Blocks))
			}
			if testCase.wantCalendar {
				location, err := time.LoadLocation(payload.Timezone)
				if err != nil {
					t.Fatalf("load Activity timezone %q: %v", payload.Timezone, err)
				}
				windowStart := payload.WindowStart.In(location)
				windowEnd := payload.WindowEnd.In(location)
				if windowStart.Hour() != 0 || windowStart.Minute() != 0 || windowStart.Second() != 0 || !windowEnd.Equal(windowStart.AddDate(0, 0, 1)) {
					t.Fatalf("Activity %s did not keep a local calendar day: %s..%s", testCase.name, windowStart, windowEnd)
				}
			}
			if testCase.wantDays > 0 {
				location, err := time.LoadLocation(payload.Timezone)
				if err != nil {
					t.Fatalf("load Activity timezone %q: %v", payload.Timezone, err)
				}
				windowStart := payload.WindowStart.In(location)
				windowEnd := payload.WindowEnd.In(location)
				if wantEnd := windowStart.AddDate(0, 0, testCase.wantDays); !windowEnd.Equal(wantEnd) {
					t.Fatalf("Activity calendar end=%s, want %s", windowEnd, wantEnd)
				}
			} else if got := payload.WindowEnd.Sub(payload.WindowStart); got != testCase.wantDuration {
				t.Fatalf("Activity window duration = %s, want %s", got, testCase.wantDuration)
			}
			if !payload.Blocks[0].StartTime.Equal(payload.WindowStart) || !payload.Blocks[len(payload.Blocks)-1].EndTime.Equal(payload.WindowEnd) {
				t.Fatalf("Activity boundaries do not match window %s..%s", payload.WindowStart, payload.WindowEnd)
			}
			for index, block := range payload.Blocks {
				if block.Rate != -1 {
					t.Fatalf("empty block %d rate = %v, want -1", index, block.Rate)
				}
				if index > 0 && !payload.Blocks[index-1].EndTime.Equal(block.StartTime) {
					t.Fatalf("blocks %d and %d are not contiguous", index-1, index)
				}
			}
		})
	}
}

func TestUsageActivityReturnsInternalErrorWhenUsageProviderIsMissing(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/activity?range=24h", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing provider status=%d, want 500: %s", response.Code, response.Body.String())
	}
}

func TestKeyActivityForcesViewerAPIKeyAndIgnoresEventsOnlyFilters(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	provider := &usageActivityRouteStub{
		UsageProvider: &usageEventsStub{},
		activity: &servicedto.UsageActivitySnapshot{
			Window:  servicedto.UsageActivityWindowWeek,
			Grain:   "medium",
			Rows:    7,
			Columns: 52,
			Blocks:  []servicedto.UsageActivityBlock{},
		},
	}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "provider-a", DisplayKey: "provider-a"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	activityResponse := httptest.NewRecorder()
	activityRequest := httptest.NewRequest(http.MethodGet, "/api/v1/key-activity?window=year&api_key_id=not-a-number&page=0&result=bogus", nil)
	activityRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(activityResponse, activityRequest)
	if activityResponse.Code != http.StatusOK {
		t.Fatalf("key Activity status=%d body=%s", activityResponse.Code, activityResponse.Body.String())
	}
	if provider.lastFilter.APIKeyID != "42" || provider.lastFilter.ActivityWindow != servicedto.UsageActivityWindowYear {
		t.Fatalf("key Activity should force the viewer API key and preserve time semantics: %+v", provider.lastFilter)
	}
}
