package test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(body, `"p95_ttft_ms":120`) || !strings.Contains(body, `"p95_latency_ms":800`) || !strings.Contains(body, `"ttft_ms":120`) {
		t.Fatalf("unexpected latency payload: %s", body)
	}
	if provider.analysisCalls != 0 || provider.latencyCalls != 1 {
		t.Fatalf("expected only latency analysis call, got analysis=%d latency=%d", provider.analysisCalls, provider.latencyCalls)
	}
	if provider.latencyFilter.Range != "24h" || provider.latencyFilter.StartTime == nil || provider.latencyFilter.EndTime == nil {
		t.Fatalf("expected resolved latency filter, got %+v", provider.latencyFilter)
	}
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
