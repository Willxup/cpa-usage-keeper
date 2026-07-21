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
		Window: servicedto.UsageActivityWindow7D, Grain: "medium", Rows: 7, Columns: 52, Blocks: []servicedto.UsageActivityBlock{},
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
	oneYearPath := "/api/v1/usage/activity?window=1y&api_key_id=42"
	router.ServeHTTP(oneYearResponse, httptest.NewRequest(http.MethodGet, oneYearPath, nil))
	if oneYearResponse.Code != http.StatusOK {
		t.Fatalf("Admin one-year Activity status=%d body=%s", oneYearResponse.Code, oneYearResponse.Body.String())
	}
	if provider.calls != 2 || provider.lastFilter.ActivityWindow != servicedto.UsageActivityWindow1Y || provider.lastFilter.APIKeyID != "42" {
		t.Fatalf("unexpected Admin one-year Activity filter: calls=%d filter=%+v", provider.calls, provider.lastFilter)
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
	router := NewRouter(nil, nil, service.NewUsageService(db), nil, AuthConfig{}, nil, "")
	testCases := []struct {
		name         string
		query        string
		wantWindow   string
		wantDuration time.Duration
		wantDays     int
	}{
		{name: "hours", query: "range=8h", wantWindow: "24h", wantDuration: 24 * time.Hour},
		{name: "today", query: "range=today", wantWindow: "24h", wantDuration: 24 * time.Hour},
		{name: "one day", query: "range=1d", wantWindow: "24h", wantDuration: 24 * time.Hour},
		{name: "two days", query: "range=2d", wantWindow: "7d", wantDuration: 7 * 24 * time.Hour},
		{name: "seven days", query: "range=7d", wantWindow: "7d", wantDuration: 7 * 24 * time.Hour},
		{name: "eight days", query: "range=8d", wantWindow: "30d", wantDuration: 30 * 24 * time.Hour},
		{name: "thirty days", query: "range=30d", wantWindow: "30d", wantDuration: 30 * 24 * time.Hour},
		{name: "one year", query: "window=1y", wantWindow: "1y", wantDays: repository.UsageActivityHeatmapBlocks},
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

func TestKeyActivityForcesViewerAPIKeyAndUsesAnIndependentRateLimitScope(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	provider := &usageActivityRouteStub{
		UsageProvider: &usageEventsStub{},
		activity: &servicedto.UsageActivitySnapshot{
			Window:  servicedto.UsageActivityWindow7D,
			Grain:   "medium",
			Rows:    7,
			Columns: 52,
			Blocks:  []servicedto.UsageActivityBlock{},
		},
	}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "provider-a", DisplayKey: "provider-a"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	overviewResponse := httptest.NewRecorder()
	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=24h", nil)
	overviewRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(overviewResponse, overviewRequest)
	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("key overview status=%d body=%s", overviewResponse.Code, overviewResponse.Body.String())
	}

	activityResponse := httptest.NewRecorder()
	activityRequest := httptest.NewRequest(http.MethodGet, "/api/v1/key-activity?window=1y&api_key_id=not-a-number&page=0&result=bogus", nil)
	activityRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	router.ServeHTTP(activityResponse, activityRequest)
	if activityResponse.Code != http.StatusOK {
		t.Fatalf("key Activity status=%d body=%s", activityResponse.Code, activityResponse.Body.String())
	}
	if provider.lastFilter.APIKeyID != "42" || provider.lastFilter.ActivityWindow != servicedto.UsageActivityWindow1Y {
		t.Fatalf("key Activity should force the viewer API key and preserve time semantics: %+v", provider.lastFilter)
	}
}
