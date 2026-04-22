package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
)

type usageEventsStub struct {
	events           []service.UsageEventRecord
	credentialStats  []service.UsageCredentialStat
	err              error
	lastFilter       service.UsageFilter
	filterCalls      int
	credentialsCalls int
}

func (s *usageEventsStub) GetUsage(context.Context) (*cpa.StatisticsSnapshot, error) {
	return nil, nil
}

func (s *usageEventsStub) GetUsageWithFilter(context.Context, service.UsageFilter) (*cpa.StatisticsSnapshot, error) {
	return nil, nil
}

func (s *usageEventsStub) ListUsageEvents(_ context.Context, filter service.UsageFilter) ([]service.UsageEventRecord, error) {
	s.lastFilter = filter
	s.filterCalls++
	return s.events, s.err
}

func (s *usageEventsStub) ListUsageCredentialStats(_ context.Context, filter service.UsageFilter) ([]service.UsageCredentialStat, error) {
	s.lastFilter = filter
	s.credentialsCalls++
	return s.credentialStats, s.err
}

func TestUsageEventsReturnsFilteredRows(t *testing.T) {
	provider := &usageEventsStub{events: []service.UsageEventRecord{{
		Timestamp:       time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:           "claude-sonnet",
		Source:          "sk-provider-key",
		AuthIndex:       "2",
		Failed:          false,
		LatencyMS:       321,
		InputTokens:     10,
		OutputTokens:    5,
		ReasoningTokens: 2,
		CachedTokens:    1,
		TotalTokens:     18,
	}}}
	router := NewRouter("", nil, provider, authFileStub{files: []models.AuthFile{{AuthIndex: "2", Email: "user@example.com", Type: "auth-file"}}}, providerMetadataStub{items: []models.ProviderMetadata{{LookupKey: "sk-provider-key", ProviderType: "openai", DisplayName: "OpenAI Mirror", ProviderKey: "openai:OpenAI Mirror"}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"events":[`) || !contains(body, `"model":"claude-sonnet"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if !contains(body, `"source":"OpenAI Mirror"`) {
		t.Fatalf("expected resolved source display in response body: %s", body)
	}
	if !contains(body, `"source_raw":"sk-provider-key"`) {
		t.Fatalf("expected raw source for filtering in response body: %s", body)
	}
	if !contains(body, `"source_type":"openai"`) {
		t.Fatalf("expected source type in response body: %s", body)
	}
	if !contains(body, `"source_key":"openai:OpenAI Mirror"`) {
		t.Fatalf("expected source key in response body: %s", body)
	}
	if !contains(body, `"auth_index":"2"`) {
		t.Fatalf("expected auth index in response body: %s", body)
	}
	if provider.filterCalls != 1 {
		t.Fatalf("expected ListUsageEvents to be called once, got %d", provider.filterCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

func TestUsageCredentialsReturnsAggregatedRows(t *testing.T) {
	provider := &usageEventsStub{credentialStats: []service.UsageCredentialStat{{
		Source:       "sk-provider-key",
		AuthIndex:    "2",
		Failed:       false,
		RequestCount: 3,
	}, {
		Source:       "sk-provider-key",
		AuthIndex:    "2",
		Failed:       true,
		RequestCount: 1,
	}}}
	router := NewRouter("", nil, provider, authFileStub{files: []models.AuthFile{{AuthIndex: "2", Email: "user@example.com", Type: "auth-file"}}}, providerMetadataStub{items: []models.ProviderMetadata{{LookupKey: "sk-provider-key", ProviderType: "openai", DisplayName: "OpenAI Mirror", ProviderKey: "openai:OpenAI Mirror"}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/credentials?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"credentials":[`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if !contains(body, `"source":"OpenAI Mirror"`) {
		t.Fatalf("expected resolved source display in response body: %s", body)
	}
	if !contains(body, `"source_type":"openai"`) {
		t.Fatalf("expected source type in response body: %s", body)
	}
	if !contains(body, `"source_key":"openai:OpenAI Mirror"`) {
		t.Fatalf("expected source key in response body: %s", body)
	}
	if !contains(body, `"success_count":3`) || !contains(body, `"failure_count":1`) || !contains(body, `"total_count":4`) {
		t.Fatalf("expected aggregated counts in response body: %s", body)
	}
	if provider.credentialsCalls != 1 {
		t.Fatalf("expected ListUsageCredentialStats to be called once, got %d", provider.credentialsCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

