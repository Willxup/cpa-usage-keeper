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
	events             []service.UsageEventRecord
	eventsPage         *service.UsageEventsPage
	eventFilterOptions *service.UsageEventFilterOptions
	credentialStats    []service.UsageCredentialStat
	err                error
	lastFilter         service.UsageFilter
	filterCalls        int
	filterOptionCalls  int
	credentialsCalls   int
}

func (s *usageEventsStub) GetUsageWithFilter(context.Context, service.UsageFilter) (*cpa.StatisticsSnapshot, error) {
	return nil, nil
}

func (s *usageEventsStub) GetUsageOverview(context.Context, service.UsageFilter) (*service.UsageOverviewSnapshot, error) {
	return nil, nil
}

func (s *usageEventsStub) ListUsageEvents(_ context.Context, filter service.UsageFilter) (*service.UsageEventsPage, error) {
	s.lastFilter = filter
	s.filterCalls++
	if s.eventsPage != nil {
		return s.eventsPage, s.err
	}
	return &service.UsageEventsPage{Events: s.events, TotalCount: int64(len(s.events)), Page: 1, PageSize: service.DefaultUsageEventsLimit, TotalPages: 1}, s.err
}

func (s *usageEventsStub) ListUsageEventFilterOptions(_ context.Context, filter service.UsageFilter) (*service.UsageEventFilterOptions, error) {
	s.lastFilter = filter
	s.filterOptionCalls++
	if s.eventFilterOptions != nil {
		return s.eventFilterOptions, s.err
	}
	return &service.UsageEventFilterOptions{}, s.err
}

func (s *usageEventsStub) ListUsageCredentialStats(_ context.Context, filter service.UsageFilter) ([]service.UsageCredentialStat, error) {
	s.lastFilter = filter
	s.credentialsCalls++
	return s.credentialStats, s.err
}

func (s *usageEventsStub) GetUsageAnalysis(context.Context, service.UsageFilter) (*service.UsageAnalysisSnapshot, error) {
	return nil, s.err
}

func TestUsageEventsReturnsFilteredRows(t *testing.T) {
	provider := &usageEventsStub{events: []service.UsageEventRecord{{
		ID:              42,
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
	if !contains(body, `"id":42`) || !contains(body, `"total_count":1`) || !contains(body, `"page":1`) || !contains(body, `"page_size":100`) || !contains(body, `"total_pages":1`) {
		t.Fatalf("expected pagination metadata and event id in response body: %s", body)
	}
	if !contains(body, `"source":"OpenAI Mirror"`) {
		t.Fatalf("expected resolved source display in response body: %s", body)
	}
	if contains(body, `sk-provider-key`) {
		t.Fatalf("expected raw source to be redacted from response body: %s", body)
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
	if provider.lastFilter.Page != 1 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 0 {
		t.Fatalf("expected default pagination to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

func TestUsageEventsPassesPaginationAndServerFilters(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{Events: []service.UsageEventRecord{}, TotalCount: 0, Page: 3, PageSize: 100, TotalPages: 0}}
	router := NewRouter("", nil, provider, nil, providerMetadataStub{items: []models.ProviderMetadata{{LookupKey: "source-a", ProviderType: "openai", DisplayName: "Provider A", ProviderKey: "openai:Provider A"}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?page=3&page_size=100&model=claude-sonnet&source=openai:Provider%20A&result=failed", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.Page != 3 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 200 {
		t.Fatalf("expected pagination filter, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.Source != "source-a" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected server-side filters, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if !contains(body, `"page":3`) || !contains(body, `"page_size":100`) || !contains(body, `"total_count":0`) || !contains(body, `"total_pages":0`) {
		t.Fatalf("expected response pagination metadata, got %s", body)
	}
}

func TestUsageEventsReturnsFilterOptions(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{
		Events: []service.UsageEventRecord{{
			ID: 7, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC), Model: "gpt-5", Source: "source-a", Failed: true,
		}},
		Models:     []string{"claude-sonnet", "gpt-5"},
		Sources:    []string{"source-a", "source-b"},
		TotalCount: 2, Page: 1, PageSize: 20, TotalPages: 1,
	}}
	router := NewRouter("", nil, provider, authFileStub{files: []models.AuthFile{{AuthIndex: "1", Email: "user@example.com", Type: "auth-file"}}}, providerMetadataStub{items: []models.ProviderMetadata{{LookupKey: "source-a", ProviderType: "openai", DisplayName: "Provider A", ProviderKey: "openai:Provider A"}, {LookupKey: "source-b", ProviderType: "anthropic", DisplayName: "Provider B", ProviderKey: "anthropic:Provider B"}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"models":["claude-sonnet","gpt-5"]`) {
		t.Fatalf("expected model filter options, got %s", body)
	}
	if !contains(body, `"sources":[`) || !contains(body, `"value":"openai:Provider A"`) || !contains(body, `"label":"Provider A"`) || !contains(body, `"value":"anthropic:Provider B"`) || !contains(body, `"label":"Provider B"`) {
		t.Fatalf("expected resolved source filter options, got %s", body)
	}
	if contains(body, `"value":"source-a"`) || contains(body, `"value":"source-b"`) {
		t.Fatalf("expected raw source filter values to be redacted, got %s", body)
	}
}

func TestUsageEventFilterOptionsReturnsStableModelsAndSources(t *testing.T) {
	provider := &usageEventsStub{eventFilterOptions: &service.UsageEventFilterOptions{
		Models:  []string{"claude-sonnet", "gpt-5"},
		Sources: []string{"source-a", "source-b"},
	}}
	router := NewRouter("", nil, provider, authFileStub{files: []models.AuthFile{{AuthIndex: "1", Email: "user@example.com", Type: "auth-file"}}}, providerMetadataStub{items: []models.ProviderMetadata{{LookupKey: "source-a", ProviderType: "openai", DisplayName: "Provider A", ProviderKey: "openai:Provider A"}, {LookupKey: "source-b", ProviderType: "anthropic", DisplayName: "Provider B", ProviderKey: "anthropic:Provider B"}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/filters?range=24h&model=ignored&source=ignored&result=failed&page=3&page_size=20", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.filterOptionCalls != 1 || provider.filterCalls != 0 {
		t.Fatalf("expected filter options endpoint only, events=%d filterOptions=%d", provider.filterCalls, provider.filterOptionCalls)
	}
	if provider.lastFilter.Range != "24h" || provider.lastFilter.Model != "" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "" || provider.lastFilter.Page != 0 || provider.lastFilter.PageSize != 0 {
		t.Fatalf("expected time range only filter, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if !contains(body, `"models":["claude-sonnet","gpt-5"]`) {
		t.Fatalf("expected stable model filter options, got %s", body)
	}
	if !contains(body, `"sources":[`) || !contains(body, `"value":"openai:Provider A"`) || !contains(body, `"label":"Provider A"`) || !contains(body, `"value":"anthropic:Provider B"`) || !contains(body, `"label":"Provider B"`) {
		t.Fatalf("expected stable resolved source filter options, got %s", body)
	}
	if contains(body, `"value":"source-a"`) || contains(body, `"value":"source-b"`) {
		t.Fatalf("expected raw source filter values to be redacted, got %s", body)
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
