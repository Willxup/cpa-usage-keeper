package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type analysisSplitStub struct {
	analysis       *servicedto.AnalysisSnapshot
	latency        *servicedto.AnalysisLatencyDiagnostics
	analysisCalls  int
	latencyCalls   int
	analysisFilter servicedto.UsageFilter
	latencyFilter  servicedto.UsageFilter
}

func (s *analysisSplitStub) GetUsageOverview(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	return nil, nil
}

func (s *analysisSplitStub) GetUsageActivity(context.Context, servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error) {
	return nil, nil
}

func (s *analysisSplitStub) GetUsageOverviewRealtime(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	return nil, nil
}

func (s *analysisSplitStub) ListUsageEvents(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	return nil, nil
}

func (s *analysisSplitStub) StreamUsageEvents(context.Context, servicedto.UsageFilter, func(servicedto.UsageEventRecord) error) error {
	return nil
}

func (s *analysisSplitStub) ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	return nil, nil
}

func (s *analysisSplitStub) GetAnalysis(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	s.analysisCalls++
	s.analysisFilter = filter
	return s.analysis, nil
}

func (s *analysisSplitStub) GetAnalysisLatency(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error) {
	s.latencyCalls++
	s.latencyFilter = filter
	return s.latency, nil
}

func TestUsageAnalysisCoreOmitsLatencyDiagnostics(t *testing.T) {
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage:  []servicedto.AnalysisTokenUsageBucket{},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if strings.Contains(response.Body.String(), `"latency_diagnostics"`) {
		t.Fatalf("expected core analysis payload to omit latency diagnostics, got %s", response.Body.String())
	}
	if provider.analysisCalls != 1 || provider.latencyCalls != 0 {
		t.Fatalf("expected only core analysis call, got analysis=%d latency=%d", provider.analysisCalls, provider.latencyCalls)
	}
}

func TestUsageAnalysisLatencyUsesIndependentRoute(t *testing.T) {
	provider := &analysisSplitStub{latency: &servicedto.AnalysisLatencyDiagnostics{
		Points:       []servicedto.AnalysisLatencyPoint{{TTFTMS: 120, LatencyMS: 800}},
		TotalPoints:  1,
		P95TTFTMS:    120,
		P95LatencyMS: 800,
		MaxTTFTMS:    120,
		MaxLatencyMS: 800,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis/latency?range=24h", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"supported":true`) || !strings.Contains(body, `"p95_ttft_ms":120`) || !strings.Contains(body, `"p95_latency_ms":800`) || !strings.Contains(body, `"ttft_ms":120`) {
		t.Fatalf("unexpected latency payload: %s", body)
	}
	if provider.analysisCalls != 0 || provider.latencyCalls != 1 {
		t.Fatalf("expected only latency analysis call, got analysis=%d latency=%d", provider.analysisCalls, provider.latencyCalls)
	}
	if provider.latencyFilter.Range != "24h" || provider.latencyFilter.StartTime == nil || provider.latencyFilter.EndTime == nil {
		t.Fatalf("expected resolved latency filter, got %+v", provider.latencyFilter)
	}
}

func TestUsageAnalysisAcceptsCustomDayRangeOlderThanThirtyDays(t *testing.T) {
	today := analysisLocalToday()
	startDay := today.AddDate(0, 0, -120)
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, analysisCustomDayURL("/api/v1/usage/analysis", startDay, today), nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected long custom Analysis range to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if provider.analysisCalls != 1 || provider.latencyCalls != 0 {
		t.Fatalf("expected only core Analysis provider call, analysis=%d latency=%d", provider.analysisCalls, provider.latencyCalls)
	}
	if provider.analysisFilter.CustomUnit != "day" || provider.analysisFilter.RangeCount != 121 {
		t.Fatalf("expected 121-day custom Analysis filter, got %+v", provider.analysisFilter)
	}
}

func TestUsageAnalysisLatencyOlderThanThirtyDaysStillCallsProvider(t *testing.T) {
	today := analysisLocalToday()
	historicalDay := today.AddDate(0, 0, -120)
	provider := &analysisSplitStub{latency: &servicedto.AnalysisLatencyDiagnostics{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, analysisCustomDayURL("/api/v1/usage/analysis/latency", historicalDay, historicalDay), nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected historical latency range to return 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"supported":true`) || strings.Contains(body, `"unsupported_reason"`) {
		t.Fatalf("expected historical latency range to stay supported, got %s", body)
	}
	if provider.latencyCalls != 1 || provider.latencyFilter.RangeCount != 1 || provider.latencyFilter.CustomUnit != "day" {
		t.Fatalf("expected historical latency range to reach provider once, got calls=%d filter=%+v", provider.latencyCalls, provider.latencyFilter)
	}
}

func TestUsageAnalysisLatencySupportsThreeHundredSixtyFiveDaysAndRejectsThreeHundredSixtySix(t *testing.T) {
	today := analysisLocalToday()
	provider := &analysisSplitStub{latency: &servicedto.AnalysisLatencyDiagnostics{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	supportedResponse := httptest.NewRecorder()
	router.ServeHTTP(supportedResponse, httptest.NewRequest(http.MethodGet, analysisCustomDayURL(
		"/api/v1/usage/analysis/latency", today.AddDate(0, 0, -364), today,
	), nil))
	if supportedResponse.Code != http.StatusOK {
		t.Fatalf("expected 365-day latency range to return 200, got %d: %s", supportedResponse.Code, supportedResponse.Body.String())
	}
	if !strings.Contains(supportedResponse.Body.String(), `"supported":true`) || provider.latencyCalls != 1 || provider.latencyFilter.RangeCount != 365 {
		t.Fatalf("expected 365-day latency provider call, calls=%d filter=%+v body=%s", provider.latencyCalls, provider.latencyFilter, supportedResponse.Body.String())
	}

	rejectedResponse := httptest.NewRecorder()
	router.ServeHTTP(rejectedResponse, httptest.NewRequest(http.MethodGet, analysisCustomDayURL(
		"/api/v1/usage/analysis/latency", today.AddDate(0, 0, -365), today,
	), nil))
	if rejectedResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected 366-day latency range to be rejected by the shared parser, got %d: %s", rejectedResponse.Code, rejectedResponse.Body.String())
	}
	if provider.latencyCalls != 1 {
		t.Fatalf("expected rejected 366-day range not to call provider, got %d calls", provider.latencyCalls)
	}
}

func TestUsageAnalysisLatencySupportsCustomRangeWithinRecentThirtyDays(t *testing.T) {
	today := analysisLocalToday()
	provider := &analysisSplitStub{latency: &servicedto.AnalysisLatencyDiagnostics{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, analysisCustomDayURL("/api/v1/usage/analysis/latency", today.AddDate(0, 0, -29), today), nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected recent 30-day latency range to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"supported":true`) {
		t.Fatalf("expected supported latency response, got %s", response.Body.String())
	}
	if provider.latencyCalls != 1 {
		t.Fatalf("expected recent latency range to reach provider once, got %d calls", provider.latencyCalls)
	}
}

func analysisLocalToday() time.Time {
	now := time.Now().In(time.Local)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}

func analysisCustomDayURL(path string, startDay, endDay time.Time) string {
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {startDay.Format(time.DateOnly)},
		"end":   {endDay.Format(time.DateOnly)},
	}
	return path + "?" + query.Encode()
}

func TestUsageAnalysisRoutesResolveRollingRangesIndependently(t *testing.T) {
	provider := &analysisSplitStub{
		analysis: &servicedto.AnalysisSnapshot{},
		latency:  &servicedto.AnalysisLatencyDiagnostics{},
	}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	coreResponse := httptest.NewRecorder()
	router.ServeHTTP(coreResponse, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil))
	if coreResponse.Code != http.StatusOK {
		t.Fatalf("expected core status 200, got %d: %s", coreResponse.Code, coreResponse.Body.String())
	}

	// 两个无状态接口各自使用服务端收到请求的时间，与 Overview 的并行加载保持一致。
	time.Sleep(5 * time.Millisecond)
	latencyResponse := httptest.NewRecorder()
	router.ServeHTTP(latencyResponse, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis/latency?range=24h", nil))
	if latencyResponse.Code != http.StatusOK {
		t.Fatalf("expected latency status 200, got %d: %s", latencyResponse.Code, latencyResponse.Body.String())
	}

	if provider.analysisFilter.StartTime == nil || provider.analysisFilter.EndTime == nil || provider.latencyFilter.StartTime == nil || provider.latencyFilter.EndTime == nil {
		t.Fatalf("expected both routes to resolve time boundaries, core=%+v latency=%+v", provider.analysisFilter, provider.latencyFilter)
	}
	if !provider.analysisFilter.StartTime.Before(*provider.latencyFilter.StartTime) || !provider.analysisFilter.EndTime.Before(*provider.latencyFilter.EndTime) {
		t.Fatalf("expected independently resolved rolling ranges, core=[%s,%s] latency=[%s,%s]",
			provider.analysisFilter.StartTime.Format(time.RFC3339Nano),
			provider.analysisFilter.EndTime.Format(time.RFC3339Nano),
			provider.latencyFilter.StartTime.Format(time.RFC3339Nano),
			provider.latencyFilter.EndTime.Format(time.RFC3339Nano),
		)
	}
}

var _ service.UsageProvider = (*analysisSplitStub)(nil)
