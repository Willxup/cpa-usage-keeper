package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
)

type usageFilterStub struct {
	usage       *cpa.StatisticsSnapshot
	err         error
	lastFilter  service.UsageFilter
	filterCalls int
}

func (s *usageFilterStub) GetUsageWithFilter(_ context.Context, filter service.UsageFilter) (*cpa.StatisticsSnapshot, error) {
	s.lastFilter = filter
	s.filterCalls++
	return s.usage, s.err
}

func (s *usageFilterStub) ListUsageEvents(context.Context, service.UsageFilter) ([]service.UsageEventRecord, error) {
	return nil, s.err
}

func (s *usageFilterStub) ListUsageCredentialStats(context.Context, service.UsageFilter) ([]service.UsageCredentialStat, error) {
	return nil, s.err
}

func (s *usageFilterStub) GetUsageAnalysis(context.Context, service.UsageFilter) (*service.UsageAnalysisSnapshot, error) {
	return nil, s.err
}

func TestUsageOverviewReturnsFilteredSnapshot(t *testing.T) {
	provider := &usageFilterStub{usage: &cpa.StatisticsSnapshot{
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
	}}
	router := NewRouter("", nil, provider, authFileStub{files: []models.AuthFile{{AuthIndex: "2", Email: "user@example.com", Type: "auth-file"}}}, nil, nil, AuthConfig{}, nil, "")
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
	if contains(body, `"details":`) {
		t.Fatalf("expected overview response to omit request details: %s", body)
	}
	if provider.filterCalls != 1 {
		t.Fatalf("expected GetUsageWithFilter to be called once, got %d", provider.filterCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}
