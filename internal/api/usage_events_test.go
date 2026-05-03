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
		AuthType:        "apikey",
		Provider:        "OpenAI Mirror",
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
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "")
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
	if contains(body, `sk-provider-key`) || contains(body, `sk-provider-prefix`) {
		t.Fatalf("expected raw source values to be redacted from response body: %s", body)
	}
	if contains(body, `"source_type"`) || !contains(body, `"source_key":"provider:OpenAI Mirror"`) {
		t.Fatalf("expected provider source key from usage event provider only, got %s", body)
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

func TestUsageEventsPassesPaginationAndProviderSourceFilter(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{Events: []service.UsageEventRecord{}, TotalCount: 0, Page: 3, PageSize: 100, TotalPages: 0}}
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?page=3&page_size=100&model=claude-sonnet&source=provider:OpenAI%20Mirror&result=failed", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.Page != 3 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 200 {
		t.Fatalf("expected pagination filter, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.AuthType != "apikey" || provider.lastFilter.Provider != "OpenAI Mirror" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected provider source filter to be translated, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if !contains(body, `"page":3`) || !contains(body, `"page_size":100`) || !contains(body, `"total_count":0`) || !contains(body, `"total_pages":0`) {
		t.Fatalf("expected response pagination metadata, got %s", body)
	}
}

func TestUsageEventsPassesAuthSourceFilter(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{Events: []service.UsageEventRecord{}, TotalCount: 0, Page: 1, PageSize: 100, TotalPages: 0}}
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?source=auth:2", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.AuthType != "oauth" || provider.lastFilter.AuthIndex != "2" || provider.lastFilter.Source != "2" {
		t.Fatalf("expected auth source filter to be translated, got %+v", provider.lastFilter)
	}
}

func TestUsageEventsRejectsEmptyPrefixedSourceFilter(t *testing.T) {
	for _, path := range []string{"/api/v1/usage/events?source=auth:", "/api/v1/usage/events?source=provider:"} {
		t.Run(path, func(t *testing.T) {
			provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{Events: []service.UsageEventRecord{}, TotalCount: 0, Page: 1, PageSize: 100, TotalPages: 0}}
			router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "")
			req := httptest.NewRequest(http.MethodGet, path, nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d with body %s", resp.Code, resp.Body.String())
			}
			if provider.filterCalls != 0 {
				t.Fatalf("expected invalid source filter not to query events, got %d calls", provider.filterCalls)
			}
		})
	}
}

func TestUsageEventsReturnsFilterOptions(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &service.UsageEventsPage{
		Events: []service.UsageEventRecord{{
			ID: 7, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC), Model: "gpt-5", AuthType: "apikey", Provider: "Provider A", Source: "source-a", Failed: true,
		}},
		Models:     []string{"claude-sonnet", "gpt-5"},
		Sources:    []string{"source-a", "source-b"},
		TotalCount: 2, Page: 1, PageSize: 20, TotalPages: 1,
	}}
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "", usageIdentitiesStub{items: []models.UsageIdentity{{ID: 1, Name: "sk-source-prefix", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "source-a", Type: "openai", Provider: "Provider A"}, {ID: 2, Name: "Provider A", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "source-b", Type: "openai", Provider: "Provider A"}, {ID: 3, Name: "Auth User", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-1", Type: "claude", Provider: "Claude"}}})
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
	if !contains(body, `"sources":[`) || !contains(body, `"value":"auth:auth-1"`) || !contains(body, `"label":"Auth User"`) || !contains(body, `"value":"provider:Provider A"`) || !contains(body, `"label":"Provider A"`) {
		t.Fatalf("expected prefixed source filter options, got %s", body)
	}
	if contains(body, `"value":"source-a"`) || contains(body, `"value":"source-b"`) || contains(body, `"provider:1"`) || contains(body, `"provider:2"`) || contains(body, `sk-source-prefix`) {
		t.Fatalf("expected raw source filter values to be redacted, got %s", body)
	}
}

func TestUsageEventFilterOptionsReturnsStableModelsAndSources(t *testing.T) {
	provider := &usageEventsStub{eventFilterOptions: &service.UsageEventFilterOptions{
		Models:  []string{"claude-sonnet", "gpt-5"},
		Sources: []string{"source-a", "source-b"},
	}}
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "", usageIdentitiesStub{items: []models.UsageIdentity{{ID: 1, Name: "sk-source-prefix", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "source-a", Type: "openai", Provider: "Provider A"}, {ID: 2, Name: "Provider A", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "source-b", Type: "openai", Provider: "Provider A"}, {ID: 3, Name: "Auth User", AuthType: models.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-1", Type: "claude", Provider: "Claude"}}})
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
	if !contains(body, `"sources":[`) || !contains(body, `"value":"auth:auth-1"`) || !contains(body, `"label":"Auth User"`) || !contains(body, `"value":"provider:Provider A"`) || !contains(body, `"label":"Provider A"`) {
		t.Fatalf("expected stable prefixed source filter options, got %s", body)
	}
	if contains(body, `"value":"source-a"`) || contains(body, `"value":"source-b"`) || contains(body, `"provider:1"`) || contains(body, `"provider:2"`) || contains(body, `sk-source-prefix`) {
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
	router := NewRouter("", nil, provider, nil, AuthConfig{}, nil, "", usageIdentitiesStub{items: []models.UsageIdentity{{ID: 1, Name: "sk-provider-prefix", AuthType: models.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "sk-provider-key", Type: "openai", Provider: "OpenAI Mirror"}}})
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
	if !contains(body, `"source_key":"provider:1"`) {
		t.Fatalf("expected source key in response body: %s", body)
	}
	if contains(body, `sk-provider-key`) || contains(body, `sk-provider-prefix`) {
		t.Fatalf("expected raw source values to be redacted from response body: %s", body)
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
