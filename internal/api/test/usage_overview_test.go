package test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type usageFilterStub struct {
	service.UsageProvider
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

type overviewAPIKeyStub struct {
	service.CPAAPIKeyProvider
	row     entities.CPAAPIKey
	findErr error
}

func (s *overviewAPIKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *overviewAPIKeyStub) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return s.row, s.findErr
}

func TestKeyOverviewIgnoresClientAPIKeyIDAndReturnsViewerOverview(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{Usage: &dto.StatisticsSnapshot{TotalRequests: 3}}}
	router, cookie := newUsageViewerRouter(t, provider)

	resp := serveAPIGet(router, "/api/v1/key-overview?range=24h&api_key_id=not-a-number", cookie)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.APIKeyID != "42" || provider.lastFilter.Range != "24h" {
		t.Fatalf("expected key overview to force viewer API key id, got %+v", provider.lastFilter)
	}
	if !strings.Contains(resp.Body.String(), `"total_requests":3`) {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}

func TestKeyOverviewRealtimeIgnoresClientAPIKeyID(t *testing.T) {
	provider := &usageFilterStub{
		realtime: &servicedto.UsageOverviewRealtime{
			Window:        "60m",
			BucketSeconds: 120,
			LatencyScatter: servicedto.RealtimeLatencyScatter{
				Points:      []servicedto.RealtimeLatencyScatterPoint{{TTFTMS: 120, LatencyMS: 800}},
				TotalPoints: 1, P95TTFTMS: 120, P95LatencyMS: 800, MaxTTFTMS: 120, MaxLatencyMS: 800,
			},
			RequestLevel: []servicedto.RealtimeRequestLevelPoint{{
				Bucket:            "2026-04-22T11:00:00Z",
				RequestsPerMinute: 6,
				Requests:          12,
			}},
		},
	}
	router, cookie := newUsageViewerRouter(t, provider)

	realtimeResp := serveAPIGet(router, "/api/v1/key-overview/realtime?window=60m&api_key_id=not-a-number", cookie)

	if realtimeResp.Code != http.StatusOK {
		t.Fatalf("expected realtime status 200, got %d %s", realtimeResp.Code, realtimeResp.Body.String())
	}
	if provider.lastRealtime.APIKeyID != "42" || provider.lastRealtime.RealtimeWindow != "60m" || provider.lastRealtime.RealtimeEndTime == nil {
		t.Fatalf("expected key overview realtime to force viewer API key id and pass window, got %+v", provider.lastRealtime)
	}
	if !strings.Contains(realtimeResp.Body.String(), `"request_level":[{"bucket":"2026-04-22T11:00:00Z","requests_per_minute":6,"requests":12}]`) {
		t.Fatalf("unexpected realtime response body: %s", realtimeResp.Body.String())
	}
	if !strings.Contains(realtimeResp.Body.String(), `"latency_scatter":{"points":[{"ttft_ms":120,"latency_ms":800}],"total_points":1,"p95_ttft_ms":120,"p95_latency_ms":800,"max_ttft_ms":120,"max_latency_ms":800}`) {
		t.Fatalf("expected key viewer scatter with paired coordinates, got %s", realtimeResp.Body.String())
	}
	for _, legacyField := range []string{`"response_level":`, `"response_distribution":`} {
		if strings.Contains(realtimeResp.Body.String(), legacyField) {
			t.Fatalf("key viewer realtime must omit %s: %s", legacyField, realtimeResp.Body.String())
		}
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
	if strings.Contains(realtimeResp.Body.String(), `"api_keys":`) || strings.Contains(realtimeResp.Body.String(), `"auth_files":`) || strings.Contains(realtimeResp.Body.String(), `"ai_providers":`) {
		t.Fatalf("expected key overview realtime to omit internal current-usage dimensions, got %s", realtimeResp.Body.String())
	}
	if provider.realtimeCalls != 1 {
		t.Fatalf("expected one realtime call, got %d", provider.realtimeCalls)
	}
}

func TestKeyOverviewRejectsUnsupportedRanges(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router, cookie := newUsageViewerRouter(t, provider)

	for _, path := range []string{"/api/v1/key-overview?range=90d", "/api/v1/key-overview?start=2026-04-20"} {
		resp := serveAPIGet(router, path, cookie)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected %s to return 400, got %d %s", path, resp.Code, resp.Body.String())
		}
	}
	if provider.overviewCalls != 0 {
		t.Fatalf("expected invalid ranges not to call usage provider, got %d", provider.overviewCalls)
	}
}

func TestKeyOverviewReturnsConflictForExpiredCustomRange(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router, cookie := newUsageViewerRouter(t, provider)

	resp := serveAPIGet(router, "/api/v1/key-overview?range=custom&unit=day&start=2000-01-01&end=2000-01-02", cookie)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected expired Custom range to return 409, got %d %s", resp.Code, resp.Body.String())
	}
	if provider.overviewCalls != 0 {
		t.Fatalf("expected expired range not to call usage provider, got %d", provider.overviewCalls)
	}
}

func TestKeyOverviewClearsInactiveViewerSession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	keyProvider := &overviewAPIKeyStub{findErr: context.Canceled}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	handler := NewAuthHandler(config, sessions)
	router := NewRouter(nil, nil, provider, nil, config, handler, "/cpa", OptionalProviders{CPAAPIKeys: keyProvider})

	resp := serveAPIGet(router, "/cpa/api/v1/key-overview?range=24h", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d %s", resp.Code, resp.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive viewer session to be deleted")
	}
	cookies := resp.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Path != "/cpa" || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected session cookie to be cleared, got %+v", cookies)
	}
}

func TestUsageOverviewResponseKeepsResolvedFilterAndTimezone(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	startDay := today.AddDate(0, 0, -6)
	startDate := startDay.Format(time.DateOnly)
	endDate := today.Format(time.DateOnly)
	resp := serveAPIGet(router, "/api/v1/usage/overview?range=custom&unit=day&start="+startDate+"&end="+endDate)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"timezone":"Asia/Shanghai"`) || strings.Contains(body, `"range_start":`) || strings.Contains(body, `"range_end":`) {
		t.Fatalf("expected overview response to retain timezone without redundant range fields, got %s", body)
	}
	if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startDay) ||
		provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(today.AddDate(0, 0, 1)) {
		t.Fatalf("expected resolved range to remain in the service filter, got %+v", provider.lastFilter)
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
	keyProvider := &overviewAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-alpha123456", KeyAlias: "Primary Key"}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keyProvider})
	resp := serveAPIGet(router, "/api/v1/usage/overview/realtime?window=15m")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"api_keys":[{"key":"42","label":"Primary Key","tokens":20,"requests":1,"share":100}]`) {
		t.Fatalf("expected realtime API key usage to use CPA API key id and alias label, got %s", body)
	}
	if strings.Contains(body, "sk-alpha123456") {
		t.Fatalf("expected realtime API key usage to avoid raw key output, got %s", body)
	}
}

func TestUsageOverviewRealtimeKeepsLegacyAPIKeyIdentifiersDistinct(t *testing.T) {
	rawKeys := []string{"sk-same-prefix-middle-one-123456", "sk-same-prefix-middle-two-123456"}
	provider := &usageFilterStub{realtime: &servicedto.UsageOverviewRealtime{
		CurrentUsage: servicedto.RealtimeCurrentUsage{APIKeys: []servicedto.RealtimeUsageTopItem{
			{Key: rawKeys[0], Tokens: 2},
			{Key: rawKeys[1], Tokens: 1},
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/overview/realtime?window=15m")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		CurrentUsage struct {
			APIKeys []struct{ Key string } `json:"api_keys"`
		} `json:"current_usage"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode realtime response: %v", err)
	}
	items := payload.CurrentUsage.APIKeys
	if len(items) != 2 || items[0].Key == items[1].Key || !strings.HasPrefix(items[0].Key, "legacy:") || !strings.HasPrefix(items[1].Key, "legacy:") {
		t.Fatalf("unexpected legacy API Key identifiers: %+v", items)
	}
	for _, rawKey := range rawKeys {
		if strings.Contains(resp.Body.String(), rawKey) {
			t.Fatalf("raw API Key leaked: %s", resp.Body.String())
		}
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
		WindowStart:   time.Date(2026, 4, 22, 11, 0, 0, 0, location),
		WindowEnd:     time.Date(2026, 4, 22, 11, 30, 0, 0, location),
		TokenVelocity: []servicedto.RealtimeTokenVelocityPoint{{
			Bucket:          "2026-04-22T11:00:00Z",
			TokensPerMinute: 120,
			Tokens:          20,
			CostUSD:         new(float64(0.123)),
		}},
		LatencyScatter: servicedto.RealtimeLatencyScatter{
			Points:      []servicedto.RealtimeLatencyScatterPoint{{TTFTMS: 120, LatencyMS: 820}},
			TotalPoints: 1, P95TTFTMS: 120, P95LatencyMS: 820, MaxTTFTMS: 120, MaxLatencyMS: 820,
		},
		CurrentUsage: servicedto.RealtimeCurrentUsage{
			Models: []servicedto.RealtimeUsageTopItem{{
				Key:      "gpt-5",
				Label:    "gpt-5",
				Tokens:   20,
				Requests: 1,
				CostUSD:  new(float64(0.123)),
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
			Bucket:              "2026-04-22T11:00:00Z",
			CacheReadRate:       new(float64(25)),
			CacheReadTokens:     5,
			CacheCreationTokens: 2,
			InputTokens:         20,
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/overview/realtime?window=30m")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if provider.lastRealtime.RealtimeWindow != "30m" || provider.lastRealtime.RealtimeEndTime == nil {
		t.Fatalf("expected realtime window and anchor to be passed through, got %+v", provider.lastRealtime)
	}
	for _, legacyField := range []string{`"response_level":`, `"response_distribution":`} {
		if strings.Contains(body, legacyField) {
			t.Fatalf("overview realtime must omit %s: %s", legacyField, body)
		}
	}
	for _, expected := range []string{
		`"window":"30m","timezone":"Asia/Shanghai","bucket_seconds":60,"window_start":"2026-04-22T11:00:00+08:00","window_end":"2026-04-22T11:30:00+08:00"`,
		`"token_velocity":[{"bucket":"2026-04-22T11:00:00Z","tokens_per_minute":120,"tokens":20,"cost":0.123}]`,
		`"latency_scatter":{"points":[{"ttft_ms":120,"latency_ms":820}],"total_points":1,"p95_ttft_ms":120,"p95_latency_ms":820,"max_ttft_ms":120,"max_latency_ms":820}`,
		`"current_usage":{"models":[{"key":"gpt-5","label":"gpt-5","tokens":20,"requests":1,"cost":0.123,"share":100}],"api_keys":[{"key":"legacy:`,
		`"request_level":[{"bucket":"2026-04-22T11:00:00Z","requests_per_minute":6,"requests":1}]`,
		`"cache_level":[{"bucket":"2026-04-22T11:00:00Z","cache_read_rate":25,"cache_read_tokens":5,"cache_creation_tokens":2,"input_tokens":20}]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected realtime response to contain %s, got %s", expected, body)
		}
	}
	if strings.Contains(body, "sk-alpha123456") {
		t.Fatalf("expected realtime API key usage to redact raw key, got %s", body)
	}
}

func TestUsageOverviewRealtimeValidatesWindowsWithAndWithoutProvider(t *testing.T) {
	for _, tc := range []struct {
		name, window string
		configured   bool
	}{
		{"nil provider accepts 60m", "60m", false},
		{"nil provider rejects 45m", "45m", false},
		{"provider rejects 5m", "5m", true},
		{"provider rejects 45m", "45m", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageFilterStub{}
			var usage service.UsageProvider
			if tc.configured {
				usage = provider
			}
			router := NewRouter(nil, nil, usage, nil, AuthConfig{}, nil, "")
			resp := serveAPIGet(router, "/api/v1/usage/overview/realtime?window="+tc.window)
			if tc.window == "60m" {
				if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"window":"60m"`) || !strings.Contains(resp.Body.String(), `"bucket_seconds":120`) {
					t.Fatalf("unexpected nil-provider realtime response: %d %s", resp.Code, resp.Body.String())
				}
			} else if resp.Code != http.StatusBadRequest || provider.realtimeCalls != 0 {
				t.Fatalf("invalid window status=%d calls=%d body=%s", resp.Code, provider.realtimeCalls, resp.Body.String())
			}
		})
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
			resp := serveAPIGet(router, tc.path)

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
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid service id", service.ErrInvalidID, http.StatusBadRequest},
		{"missing active api key", gorm.ErrRecordNotFound, http.StatusNotFound},
	} {
		for _, path := range []string{"/api/v1/usage/overview?range=24h&api_key_id=123", "/api/v1/usage/overview/realtime?window=60m&api_key_id=123"} {
			t.Run(tc.name+path, func(t *testing.T) {
				router := NewRouter(nil, nil, &usageFilterStub{err: tc.err}, nil, AuthConfig{}, nil, "")
				resp := serveAPIGet(router, path)
				if resp.Code != tc.status {
					t.Fatalf("status=%d, want %d body=%s", resp.Code, tc.status, resp.Body.String())
				}
			})
		}
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
			RPM:                 1.0 / 1440.0,
			TPM:                 20.0 / 1440.0,
			TotalCost:           0.123,
			CostAvailable:       true,
			InputTokens:         11,
			CacheReadTokens:     2,
			CacheCreationTokens: 1,
			ReasoningTokens:     3,
		},
		Series: servicedto.UsageOverviewSeries{
			Buckets:       []string{"2026-04-22T11:00:00Z"},
			Requests:      []int64{1},
			Tokens:        []int64{20},
			RPM:           []float64{1.0 / 60.0},
			TPM:           []float64{20.0 / 60.0},
			Cost:          []float64{0.123},
			CacheReadRate: []*float64{new(float64(18.18))},
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/overview?range=24h")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"usage":`) || !strings.Contains(body, `"total_requests":1`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if !strings.Contains(body, `"summary":{"rpm":`) {
		t.Fatalf("expected backend summary in response body: %s", body)
	}
	if !strings.Contains(body, `"cost_available":true`) {
		t.Fatalf("expected backend cost availability in response body: %s", body)
	}
	if !strings.Contains(body, `"input_tokens":11`) {
		t.Fatalf("expected summary input tokens in response body: %s", body)
	}
	if !strings.Contains(body, `"series":{"buckets":["2026-04-22T11:00:00Z"],"requests":[1]`) {
		t.Fatalf("expected backend series in response body: %s", body)
	}
	if !strings.Contains(body, `"cache_read_rate":[18.18]`) {
		t.Fatalf("expected backend cache-rate series in response body: %s", body)
	}
	if strings.Contains(body, `"service_health":`) {
		t.Fatalf("expected overview response to omit Activity health: %s", body)
	}
	assertUsageOverviewResponseShape(t, body)
	if strings.Contains(body, `"details":`) {
		t.Fatalf("expected overview response to omit request details: %s", body)
	}
	if strings.Contains(body, `"apis":`) || strings.Contains(body, "sk-alpha123456") {
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

func TestUsageOverviewReturnsDailyAverageSummaryFields(t *testing.T) {
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{
		Usage: &dto.StatisticsSnapshot{
			TotalRequests: 14,
			SuccessCount:  14,
			TotalTokens:   7000000,
		},
		Summary: servicedto.UsageOverviewSummary{
			RPM:                   14.0 / 10080.0,
			TPM:                   7000000.0 / 10080.0,
			TotalCost:             56.49,
			CostAvailable:         false,
			InputTokens:           7000000,
			DailyAverageRequests:  new(float64(2)),
			DailyAverageTokens:    new(float64(1000000)),
			DailyAverageCost:      new(float64(8.07)),
			DailyAverageRangeDays: new(float64(7)),
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/overview?range=7d")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"daily_average_requests":2`) ||
		!strings.Contains(body, `"daily_average_tokens":1000000`) ||
		!strings.Contains(body, `"daily_average_cost":8.07`) ||
		!strings.Contains(body, `"daily_average_range_days":7`) {
		t.Fatalf("expected daily average summary fields in response body: %s", body)
	}
	assertUsageOverviewResponseShape(t, body)
}

func TestUsageOverviewNilProviderReturnsPrunedShape(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/overview")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"summary":{"rpm":0`) || !strings.Contains(body, `"input_tokens":0`) {
		t.Fatalf("expected empty overview summary to include input_tokens, got %s", body)
	}
	if !strings.Contains(body, `"series":{"buckets":[]`) || !strings.Contains(body, `"cache_read_rate":[]`) {
		t.Fatalf("expected empty overview series to include cache_read_rate, got %s", body)
	}
	assertUsageOverviewResponseShape(t, body)
}

func assertUsageOverviewResponseShape(t *testing.T, body string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("failed to decode overview response: %v\n%s", err, body)
	}
	assertAllowedJSONKeys(t, decoded, "overview response", body, "usage", "summary", "series", "timezone")

	for _, field := range []struct {
		name string
		keys []string
	}{
		{"usage", []string{"total_requests", "success_count", "failure_count", "total_tokens"}},
		{"summary", []string{"rpm", "tpm", "total_cost", "cost_available", "input_tokens", "cache_read_tokens", "cache_creation_tokens", "reasoning_tokens", "daily_average_requests", "daily_average_tokens", "daily_average_cost", "daily_average_range_days"}},
		{"series", []string{"buckets", "requests", "tokens", "rpm", "tpm", "cost", "cache_read_rate"}},
	} {
		object, ok := decoded[field.name].(map[string]any)
		if !ok {
			t.Fatalf("expected %s object in response: %s", field.name, body)
		}
		assertAllowedJSONKeys(t, object, "overview "+field.name, body, field.keys...)
	}
}

func assertAllowedJSONKeys(t *testing.T, values map[string]any, label, body string, allowedKeys ...string) {
	t.Helper()
	for key := range values {
		if !slices.Contains(allowedKeys, key) {
			t.Fatalf("unexpected %s field %q in response: %s", label, key, body)
		}
	}
}
