package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/cpa"
)

type usageStub struct {
	usage *cpa.StatisticsSnapshot
	err   error
}

func (s usageStub) GetUsage(context.Context) (*cpa.StatisticsSnapshot, error) {
	return s.usage, s.err
}

func TestUsageReturnsEmptyStructureWithoutProvider(t *testing.T) {
	router := NewRouter("", nil, nil, nil, AuthConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"usage":`) && contains(body, `"total_requests":0`) && contains(body, `"apis":{}`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestUsageReturnsAggregatedSnapshot(t *testing.T) {
	router := NewRouter("", nil, usageStub{usage: &cpa.StatisticsSnapshot{
		TotalRequests: 2,
		SuccessCount:  1,
		FailureCount:  1,
		TotalTokens:   40,
		APIs: map[string]cpa.APISnapshot{
			"provider-a": {
				TotalRequests: 2,
				TotalTokens:   40,
				Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {TotalRequests: 2, TotalTokens: 40},
				},
			},
		},
	}}, nil, AuthConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"total_requests":2`) && contains(body, `"success_count":1`) && contains(body, `"provider-a"`) && contains(body, `"claude-sonnet"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}
