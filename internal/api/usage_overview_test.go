package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type usageFilterStub struct {
	overview      *servicedto.UsageOverviewSnapshot
	realtime      *servicedto.UsageOverviewRealtime
	err           error
	lastFilter    servicedto.UsageFilter
	lastRealtime  servicedto.UsageFilter
	overviewCalls int
	realtimeCalls int
}

func (s *usageFilterStub) GetUsageOverview(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	s.lastFilter = filter
	s.overviewCalls++
	return s.overview, s.err
}

func (s *usageFilterStub) GetUsageOverviewRealtime(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	s.lastRealtime = filter
	s.realtimeCalls++
	return s.realtime, s.err
}

func (s *usageFilterStub) ListUsageEvents(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	return nil, s.err
}

func (s *usageFilterStub) ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	return nil, s.err
}

func (s *usageFilterStub) GetAnalysis(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	return nil, s.err
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse returned error: %v", err)
	}
	return parsed
}

func TestKeyOverviewForcesViewerAPIKeyIDAndReturnsOverview(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{Usage: &dto.StatisticsSnapshot{TotalRequests: 3}}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=24h&api_key_id=999", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.APIKeyID != "42" || provider.lastFilter.Range != "24h" {
		t.Fatalf("expected key overview to force viewer API key id, got %+v", provider.lastFilter)
	}
	if !contains(resp.Body.String(), `"total_requests":3`) {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}

func TestKeyOverviewRealtimeForcesViewerAPIKeyIDAndAllowsParallelOverviewRequest(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{
		overview: &servicedto.UsageOverviewSnapshot{Usage: &dto.StatisticsSnapshot{TotalRequests: 3}},
		realtime: &servicedto.UsageOverviewRealtime{
			Window:        "60m",
			BucketSeconds: 120,
			RequestLevel: []servicedto.RealtimeRequestLevelPoint{{
				Bucket:            "2026-04-22T11:00:00Z",
				RequestsPerMinute: 6,
				Requests:          12,
			}},
		},
	}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	overviewResp := httptest.NewRecorder()
	overviewReq := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=24h", nil)
	overviewReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(overviewResp, overviewReq)
	if overviewResp.Code != http.StatusOK {
		t.Fatalf("expected overview status 200, got %d %s", overviewResp.Code, overviewResp.Body.String())
	}

	realtimeResp := httptest.NewRecorder()
	realtimeReq := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview/realtime?window=60m&api_key_id=999", nil)
	realtimeReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(realtimeResp, realtimeReq)

	if realtimeResp.Code != http.StatusOK {
		t.Fatalf("expected realtime status 200, got %d %s", realtimeResp.Code, realtimeResp.Body.String())
	}
	if provider.lastRealtime.APIKeyID != "42" || provider.lastRealtime.RealtimeWindow != "60m" || provider.lastRealtime.RealtimeEndTime == nil {
		t.Fatalf("expected key overview realtime to force viewer API key id and pass window, got %+v", provider.lastRealtime)
	}
	if !contains(realtimeResp.Body.String(), `"request_level":[{"bucket":"2026-04-22T11:00:00Z","requests_per_minute":6,"requests":12}]`) {
		t.Fatalf("unexpected realtime response body: %s", realtimeResp.Body.String())
	}
	var realtimeBody map[string]any
	if err := json.Unmarshal(realtimeResp.Body.Bytes(), &realtimeBody); err != nil {
		t.Fatalf("decode key overview realtime response: %v", err)
	}
	currentUsage, ok := realtimeBody["current_usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected key overview realtime current_usage object, got %s", realtimeResp.Body.String())
	}
	assertAllowedJSONKeys(t, currentUsage, "key overview realtime current_usage", realtimeResp.Body.String(), "models")
	if contains(realtimeResp.Body.String(), `"api_keys":`) || contains(realtimeResp.Body.String(), `"auth_files":`) || contains(realtimeResp.Body.String(), `"ai_providers":`) {
		t.Fatalf("expected key overview realtime to omit internal current-usage dimensions, got %s", realtimeResp.Body.String())
	}
	if provider.overviewCalls != 1 || provider.realtimeCalls != 1 {
		t.Fatalf("expected one overview and one realtime call, got overview=%d realtime=%d", provider.overviewCalls, provider.realtimeCalls)
	}
}

func TestKeyOverviewRejectsCustomAndUnsupportedRanges(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, path := range []string{"/api/v1/key-overview?range=custom&start=2026-04-20&end=2026-04-21", "/api/v1/key-overview?range=90d", "/api/v1/key-overview?start=2026-04-20"} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected %s to return 400, got %d %s", path, resp.Code, resp.Body.String())
		}
	}
	if provider.overviewCalls != 0 {
		t.Fatalf("expected invalid ranges not to call usage provider, got %d", provider.overviewCalls)
	}
}

func TestKeyOverviewRateLimitsPerViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, DisplayKey: "sk-*********live"}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for i, expected := range []int{http.StatusOK, http.StatusTooManyRequests} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview?range=24h", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		router.ServeHTTP(resp, req)
		if resp.Code != expected {
			t.Fatalf("request %d expected %d, got %d %s", i+1, expected, resp.Code, resp.Body.String())
		}
	}
}

func TestKeyOverviewClearsInactiveViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	keyProvider := &authCPAAPIKeyStub{findErr: context.Canceled}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, provider, nil, config, handler, "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	if !handler.allowKeyOverviewRequest(token) {
		t.Fatal("expected initial key overview request to be allowed")
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cpa/api/v1/key-overview?range=24h", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d %s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive viewer session to be deleted")
	}
	if _, ok := handler.keyOverviewRequests[token]; ok {
		t.Fatal("expected inactive key overview cleanup to clear rate limit entry")
	}
	cookies := resp.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Path != "/cpa" || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected session cookie to be cleared, got %+v", cookies)
	}
}

func TestUsageOverviewResponseIncludesResolvedRangeAndTimezone(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	retentionStart, ok := usageFilterRetentionStart(time.Now())
	if !ok {
		t.Fatal("expected current time to provide a retention start")
	}
	startDate := retentionStart.Format(time.DateOnly)
	endDate := retentionStart.AddDate(0, 0, 1).Format(time.DateOnly)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=custom&start="+startDate+"&end="+endDate, nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	expectedStart := retentionStart.Format(time.RFC3339Nano)
	expectedEnd := retentionStart.AddDate(0, 0, 2).Add(-time.Nanosecond).Format(time.RFC3339Nano)
	body := resp.Body.String()
	if !contains(body, `"timezone":"Asia/Shanghai"`) || !contains(body, `"range_start":"`+expectedStart+`"`) || !contains(body, `"range_end":"`+expectedEnd+`"`) {
		t.Fatalf("expected overview response to include resolved range and timezone, got %s", body)
	}
}

func TestUsageOverviewRealtimeUsesCPAAPIKeyAliasLabels(t *testing.T) {
	provider := &usageFilterStub{realtime: &servicedto.UsageOverviewRealtime{
		Window:        "15m",
		BucketSeconds: 30,
		CurrentUsage: servicedto.RealtimeCurrentUsage{
			APIKeys: []servicedto.RealtimeUsageTopItem{{
				Key:      "sk-alpha123456",
				Label:    "sk-alpha123456",
				Tokens:   20,
				Requests: 1,
				Share:    100,
			}},
		},
	}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-alpha123456", KeyAlias: "Primary Key"}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keyProvider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=15m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !contains(body, `"api_keys":[{"key":"42","label":"Primary Key","tokens":20,"requests":1,"share":100}]`) {
		t.Fatalf("expected realtime API key usage to use CPA API key id and alias label, got %s", body)
	}
	if contains(body, "sk-alpha123456") {
		t.Fatalf("expected realtime API key usage to avoid raw key output, got %s", body)
	}
}

func TestUsageOverviewRealtimeAcceptsWindowAndReturnsRealtimeBlock(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageFilterStub{realtime: &servicedto.UsageOverviewRealtime{
		Window:        "30m",
		BucketSeconds: 60,
		TokenVelocity: []servicedto.RealtimeTokenVelocityPoint{{
			Bucket:          "2026-04-22T11:00:00Z",
			TokensPerMinute: 120,
			Tokens:          20,
			CostUSD:         float64Ptr(0.123),
		}},
		ResponseLevel: []servicedto.RealtimeResponseLevelPoint{{
			Bucket:       "2026-04-22T11:00:00Z",
			TTFTP95MS:    int64Ptr(210),
			LatencyP95MS: int64Ptr(820),
		}},
		CurrentUsage: servicedto.RealtimeCurrentUsage{
			Models: []servicedto.RealtimeUsageTopItem{{
				Key:      "gpt-5",
				Label:    "gpt-5",
				Tokens:   20,
				Requests: 1,
				CostUSD:  float64Ptr(0.123),
				Share:    100,
			}},
			APIKeys: []servicedto.RealtimeUsageTopItem{{
				Key:      "sk-alpha123456",
				Label:    "sk-alpha123456",
				Tokens:   20,
				Requests: 1,
				Share:    100,
			}},
		},
		RequestLevel: []servicedto.RealtimeRequestLevelPoint{{
			Bucket:            "2026-04-22T11:00:00Z",
			RequestsPerMinute: 6,
			Requests:          1,
		}},
		CacheLevel: []servicedto.RealtimeCacheLevelPoint{{
			Bucket:       "2026-04-22T11:00:00Z",
			CacheRate:    float64Ptr(25),
			CachedTokens: 5,
			InputTokens:  20,
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=30m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if provider.lastRealtime.RealtimeWindow != "30m" || provider.lastRealtime.RealtimeEndTime == nil {
		t.Fatalf("expected realtime window and anchor to be passed through, got %+v", provider.lastRealtime)
	}
	for _, expected := range []string{
		`"window":"30m","timezone":"Asia/Shanghai","bucket_seconds":60`,
		`"token_velocity":[{"bucket":"2026-04-22T11:00:00Z","tokens_per_minute":120,"tokens":20,"cost":0.123}]`,
		`"response_level":[{"bucket":"2026-04-22T11:00:00Z","ttft_p95_ms":210,"latency_p95_ms":820}]`,
		`"response_distribution":{"ttft":{"average_line":[],"particles":[]},"latency":{"average_line":[],"particles":[]}}`,
		`"current_usage":{"models":[{"key":"gpt-5","label":"gpt-5","tokens":20,"requests":1,"cost":0.123,"share":100}],"api_keys":[{"key":"sk-*********123456","label":"sk-*********123456","tokens":20,"requests":1,"share":100}]`,
		`"request_level":[{"bucket":"2026-04-22T11:00:00Z","requests_per_minute":6,"requests":1}]`,
		`"cache_level":[{"bucket":"2026-04-22T11:00:00Z","cache_rate":25,"cached_tokens":5,"input_tokens":20}]`,
	} {
		if !contains(body, expected) {
			t.Fatalf("expected realtime response to contain %s, got %s", expected, body)
		}
	}
	if contains(body, "sk-alpha123456") {
		t.Fatalf("expected realtime API key usage to redact raw key, got %s", body)
	}
}

func TestUsageOverviewRealtimeNilProviderStillParsesWindow(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=60m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	if !contains(resp.Body.String(), `"window":"60m"`) {
		t.Fatalf("expected nil provider realtime response to keep requested window, got %s", resp.Body.String())
	}
	if !contains(resp.Body.String(), `"bucket_seconds":120`) {
		t.Fatalf("expected nil provider realtime response to include 60m bucket seconds, got %s", resp.Body.String())
	}
}

func TestUsageOverviewRealtimeNilProviderRejectsUnsupportedWindow(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=45m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUsageOverviewRealtimeRejectsFiveMinuteWindow(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=5m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.realtimeCalls != 0 {
		t.Fatalf("expected removed 5m realtime window not to call usage provider, got %d", provider.realtimeCalls)
	}
}

func TestUsageOverviewRejectsInvalidAPIKeyID(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	tests := []struct {
		name string
		path string
	}{
		{name: "overview", path: "/api/v1/usage/overview?range=24h&api_key_id=not-an-id"},
		{name: "realtime", path: "/api/v1/usage/overview/realtime?window=60m&api_key_id=not-an-id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected %s to return 400, got %d %s", tc.path, resp.Code, resp.Body.String())
			}
		})
	}

	if provider.overviewCalls != 0 || provider.realtimeCalls != 0 {
		t.Fatalf("expected invalid api_key_id not to call usage provider, got overview=%d realtime=%d", provider.overviewCalls, provider.realtimeCalls)
	}
}

func TestUsageOverviewMapsAPIKeyLookupErrors(t *testing.T) {
	tests := []struct {
		name       string
		provider   *usageFilterStub
		wantStatus int
	}{
		{name: "invalid service id", provider: &usageFilterStub{err: service.ErrInvalidID}, wantStatus: http.StatusBadRequest},
		{name: "missing active api key", provider: &usageFilterStub{err: gorm.ErrRecordNotFound}, wantStatus: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name+"/overview", func(t *testing.T) {
			router := NewRouter(nil, nil, tc.provider, nil, AuthConfig{}, nil, "")
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=24h&api_key_id=123", nil)
			router.ServeHTTP(resp, req)

			if resp.Code != tc.wantStatus {
				t.Fatalf("expected overview status %d, got %d %s", tc.wantStatus, resp.Code, resp.Body.String())
			}
		})

		t.Run(tc.name+"/realtime", func(t *testing.T) {
			router := NewRouter(nil, nil, tc.provider, nil, AuthConfig{}, nil, "")
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=60m&api_key_id=123", nil)
			router.ServeHTTP(resp, req)

			if resp.Code != tc.wantStatus {
				t.Fatalf("expected realtime status %d, got %d %s", tc.wantStatus, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestUsageOverviewRejectsUnsupportedRealtimeWindow(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/realtime?window=45m", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.realtimeCalls != 0 {
		t.Fatalf("expected unsupported realtime window not to call usage provider, got %d", provider.realtimeCalls)
	}
}

func TestUsageOverviewReturnsFilteredSnapshot(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{
		Usage: &dto.StatisticsSnapshot{
			TotalRequests: 1,
			SuccessCount:  1,
			TotalTokens:   20,
		},
		Summary: servicedto.UsageOverviewSummary{
			RequestCount:    1,
			TokenCount:      20,
			WindowMinutes:   1440,
			RPM:             1.0 / 1440.0,
			TPM:             20.0 / 1440.0,
			TotalCost:       0.123,
			CostAvailable:   true,
			InputTokens:     11,
			CachedTokens:    2,
			ReasoningTokens: 3,
		},
		Series: servicedto.UsageOverviewSeries{
			Requests:  map[string]int64{"2026-04-22T11:00:00Z": 1},
			Tokens:    map[string]int64{"2026-04-22T11:00:00Z": 20},
			RPM:       map[string]float64{"2026-04-22T11:00:00Z": 1.0 / 60.0},
			TPM:       map[string]float64{"2026-04-22T11:00:00Z": 20.0 / 60.0},
			Cost:      map[string]float64{"2026-04-22T11:00:00Z": 0.123},
			CacheRate: map[string]*float64{"2026-04-22T11:00:00Z": float64Ptr(18.18)},
		},
		Health: servicedto.UsageOverviewHealth{
			TotalSuccess: 1,
			TotalFailure: 0,
			SuccessRate:  100,
			BlockDetails: []servicedto.UsageOverviewHealthBlock{{
				StartTime: mustParseTime(t, "2026-04-22T11:00:00Z"),
				EndTime:   mustParseTime(t, "2026-04-22T11:15:00Z"),
				Success:   1,
				Failure:   0,
				Rate:      1,
			}},
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"usage":`) || !contains(body, `"total_requests":1`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if !contains(body, `"summary":{"request_count":1,"token_count":20`) {
		t.Fatalf("expected backend summary in response body: %s", body)
	}
	if !contains(body, `"cost_available":true`) {
		t.Fatalf("expected backend cost availability in response body: %s", body)
	}
	if !contains(body, `"input_tokens":11`) {
		t.Fatalf("expected summary input tokens in response body: %s", body)
	}
	if !contains(body, `"series":{"requests":{"2026-04-22T11:00:00Z":1}`) {
		t.Fatalf("expected backend series in response body: %s", body)
	}
	if !contains(body, `"cache_rate":{"2026-04-22T11:00:00Z":18.18}`) {
		t.Fatalf("expected backend cache-rate series in response body: %s", body)
	}
	if !contains(body, `"service_health":{"total_success":1,"total_failure":0,"success_rate":100`) ||
		!contains(body, `"block_details":[{"start_time":"2026-04-22T11:00:00Z","end_time":"2026-04-22T11:15:00Z","success":1,"failure":0,"rate":1}]`) {
		t.Fatalf("expected service health in response body: %s", body)
	}
	assertUsageOverviewResponseShape(t, body)
	if contains(body, `"details":`) {
		t.Fatalf("expected overview response to omit request details: %s", body)
	}
	if contains(body, `"apis":`) || contains(body, "sk-alpha123456") {
		t.Fatalf("expected overview response to omit api key dimension: %s", body)
	}
	if provider.overviewCalls != 1 {
		t.Fatalf("expected GetUsageOverview to be called once, got %d", provider.overviewCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

func TestUsageOverviewNilProviderReturnsPrunedShape(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"summary":{"request_count":0`) || !contains(body, `"input_tokens":0`) {
		t.Fatalf("expected empty overview summary to include input_tokens, got %s", body)
	}
	if !contains(body, `"series":{"requests":{}`) || !contains(body, `"cache_rate":{}`) {
		t.Fatalf("expected empty overview series to include cache_rate, got %s", body)
	}
	assertUsageOverviewResponseShape(t, body)
}

func assertUsageOverviewResponseShape(t *testing.T, body string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("failed to decode overview response: %v\n%s", err, body)
	}
	assertAllowedJSONKeys(t, decoded, "overview response", body, "usage", "summary", "series", "service_health", "timezone", "range_start", "range_end")

	usage, ok := decoded["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage object in response, got %s", body)
	}
	assertAllowedJSONKeys(t, usage, "overview usage", body, "total_requests", "success_count", "failure_count", "total_tokens")

	series, ok := decoded["series"].(map[string]any)
	if !ok {
		t.Fatalf("expected series object in response, got %s", body)
	}
	assertAllowedJSONKeys(t, series, "overview series", body, "requests", "tokens", "rpm", "tpm", "cost", "cache_rate")
}

func assertAllowedJSONKeys(t *testing.T, values map[string]any, label, body string, allowedKeys ...string) {
	t.Helper()
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("unexpected %s field %q in response: %s", label, key, body)
		}
	}
}
func float64Ptr(value float64) *float64 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
