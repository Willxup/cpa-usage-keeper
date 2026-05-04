package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/service"
)

type usageFilterStub struct {
	usage         *cpa.StatisticsSnapshot
	overview      *service.UsageOverviewSnapshot
	err           error
	lastFilter    service.UsageFilter
	filterCalls   int
	overviewCalls int
}

func (s *usageFilterStub) GetUsageWithFilter(_ context.Context, filter service.UsageFilter) (*cpa.StatisticsSnapshot, error) {
	s.lastFilter = filter
	s.filterCalls++
	return s.usage, s.err
}

func (s *usageFilterStub) GetUsageOverview(_ context.Context, filter service.UsageFilter) (*service.UsageOverviewSnapshot, error) {
	s.lastFilter = filter
	s.overviewCalls++
	return s.overview, s.err
}

func (s *usageFilterStub) ListUsageEvents(context.Context, service.UsageFilter) (*service.UsageEventsPage, error) {
	return nil, s.err
}

func (s *usageFilterStub) ListUsageEventFilterOptions(context.Context, service.UsageFilter) (*service.UsageEventFilterOptions, error) {
	return nil, s.err
}

func (s *usageFilterStub) ListUsageCredentialStats(context.Context, service.UsageFilter) ([]service.UsageCredentialStat, error) {
	return nil, s.err
}

func (s *usageFilterStub) GetUsageAnalysis(context.Context, service.UsageFilter) (*service.UsageAnalysisSnapshot, error) {
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

func TestUsageOverviewResponseIncludesResolvedRangeAndTimezone(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageFilterStub{overview: &service.UsageOverviewSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=custom&start=2026-04-20&end=2026-04-21", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	expectedStart := time.Date(2026, 4, 20, 0, 0, 0, 0, location).UTC().Format(time.RFC3339Nano)
	expectedEnd := time.Date(2026, 4, 22, 0, 0, 0, 0, location).Add(-time.Nanosecond).UTC().Format(time.RFC3339Nano)
	body := resp.Body.String()
	if !contains(body, `"timezone":"Asia/Shanghai"`) || !contains(body, `"range_start":"`+expectedStart+`"`) || !contains(body, `"range_end":"`+expectedEnd+`"`) {
		t.Fatalf("expected overview response to include resolved range and timezone, got %s", body)
	}
}

func TestUsageOverviewReturnsFilteredSnapshot(t *testing.T) {
	provider := &usageFilterStub{overview: &service.UsageOverviewSnapshot{
		Usage: &cpa.StatisticsSnapshot{
			TotalRequests: 1,
			SuccessCount:  1,
			TotalTokens:   20,
			RequestsByHour: map[string]int64{
				"2026-04-22T11:00:00Z": 1,
			},
			TokensByHour: map[string]int64{
				"2026-04-22T11:00:00Z": 20,
			},
			APIs: map[string]cpa.APISnapshot{
				"provider-a": {
					TotalRequests: 1,
					SuccessCount:  1,
					TotalTokens:   20,
					Models: map[string]cpa.ModelSnapshot{
						"claude-sonnet": {
							TotalRequests: 1,
							SuccessCount:  1,
							TotalTokens:   20,
						},
					},
				},
			},
		},
		Summary: service.UsageOverviewSummary{
			RequestCount:    1,
			TokenCount:      20,
			WindowMinutes:   1440,
			RPM:             1.0 / 1440.0,
			TPM:             20.0 / 1440.0,
			TotalCost:       0.123,
			CostAvailable:   true,
			CachedTokens:    2,
			ReasoningTokens: 3,
		},
		Series: service.UsageOverviewSeries{
			Requests:        map[string]int64{"2026-04-22T11:00:00Z": 1},
			Tokens:          map[string]int64{"2026-04-22T11:00:00Z": 20},
			RPM:             map[string]float64{"2026-04-22T11:00:00Z": 1.0 / 60.0},
			TPM:             map[string]float64{"2026-04-22T11:00:00Z": 20.0 / 60.0},
			Cost:            map[string]float64{"2026-04-22T11:00:00Z": 0.123},
			InputTokens:     map[string]int64{"2026-04-22T11:00:00Z": 11},
			OutputTokens:    map[string]int64{"2026-04-22T11:00:00Z": 7},
			CachedTokens:    map[string]int64{"2026-04-22T11:00:00Z": 2},
			ReasoningTokens: map[string]int64{"2026-04-22T11:00:00Z": 3},
		},
		Health: service.UsageOverviewHealth{
			TotalSuccess: 1,
			TotalFailure: 0,
			SuccessRate:  100,
			BlockDetails: []service.UsageOverviewHealthBlock{{
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
	if !contains(body, `"series":{"requests":{"2026-04-22T11:00:00Z":1}`) {
		t.Fatalf("expected backend series in response body: %s", body)
	}
	if !contains(body, `"input_tokens":{"2026-04-22T11:00:00Z":11}`) ||
		!contains(body, `"output_tokens":{"2026-04-22T11:00:00Z":7}`) ||
		!contains(body, `"cached_tokens":{"2026-04-22T11:00:00Z":2}`) ||
		!contains(body, `"reasoning_tokens":{"2026-04-22T11:00:00Z":3}`) {
		t.Fatalf("expected token breakdown series in response body: %s", body)
	}
	if !contains(body, `"service_health":{"total_success":1,"total_failure":0,"success_rate":100`) ||
		!contains(body, `"block_details":[{"start_time":"2026-04-22T11:00:00Z","end_time":"2026-04-22T11:15:00Z","success":1,"failure":0,"rate":1}]`) {
		t.Fatalf("expected service health in response body: %s", body)
	}
	if contains(body, `"details":`) {
		t.Fatalf("expected overview response to omit request details: %s", body)
	}
	if provider.filterCalls != 0 {
		t.Fatalf("expected GetUsageWithFilter not to be called, got %d", provider.filterCalls)
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
