package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type analysisSplitStub struct {
	service.UsageProvider
	analysis       *servicedto.AnalysisSnapshot
	latency        *servicedto.AnalysisLatencyDiagnostics
	analysisCalls  int
	latencyCalls   int
	analysisFilter servicedto.UsageFilter
	latencyFilter  servicedto.UsageFilter
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
	response := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")

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
	response := serveAPIGet(router, "/api/v1/usage/analysis/latency?range=24h")

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
	response := serveAPIGet(router, analysisCustomDayURL("/api/v1/usage/analysis", startDay, today))

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

func TestUsageAnalysisLatencyCustomDayBounds(t *testing.T) {
	today := analysisLocalToday()
	for _, tc := range []struct {
		name                         string
		startOffset, endOffset, days int
		status                       int
	}{
		{"historical day", -120, -120, 1, http.StatusOK},
		{"recent thirty days", -29, 0, 30, http.StatusOK},
		{"maximum days", -364, 0, 365, http.StatusOK},
		{"too many days", -365, 0, 366, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &analysisSplitStub{latency: &servicedto.AnalysisLatencyDiagnostics{}}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			response := serveAPIGet(router, analysisCustomDayURL("/api/v1/usage/analysis/latency", today.AddDate(0, 0, tc.startOffset), today.AddDate(0, 0, tc.endOffset)))
			if response.Code != tc.status {
				t.Fatalf("status=%d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.status == http.StatusBadRequest {
				if provider.latencyCalls != 0 {
					t.Fatalf("rejected range called provider %d times", provider.latencyCalls)
				}
				return
			}
			body := response.Body.String()
			if !strings.Contains(body, `"supported":true`) || strings.Contains(body, `"unsupported_reason"`) {
				t.Fatalf("expected supported latency response: %s", body)
			}
			if provider.latencyCalls != 1 || provider.latencyFilter.RangeCount != tc.days || provider.latencyFilter.CustomUnit != "day" {
				t.Fatalf("expected %d-day provider call, got calls=%d filter=%+v", tc.days, provider.latencyCalls, provider.latencyFilter)
			}
		})
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
	synctest.Test(t, func(t *testing.T) {
		provider := &analysisSplitStub{
			analysis: &servicedto.AnalysisSnapshot{},
			latency:  &servicedto.AnalysisLatencyDiagnostics{},
		}
		router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

		coreResponse := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")
		if coreResponse.Code != http.StatusOK {
			t.Fatalf("expected core status 200, got %d: %s", coreResponse.Code, coreResponse.Body.String())
		}

		// 两个无状态接口各自使用服务端收到请求的时间，与 Overview 的并行加载保持一致。
		time.Sleep(time.Second)
		latencyResponse := serveAPIGet(router, "/api/v1/usage/analysis/latency?range=24h")
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
	})
}
