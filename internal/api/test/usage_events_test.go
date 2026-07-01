package test

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageEventsStub struct {
	events             []servicedto.UsageEventRecord
	eventsPage         *servicedto.UsageEventsPage
	exportEvents       []servicedto.UsageEventRecord
	eventFilterOptions *servicedto.UsageEventFilterOptions
	err                error
	lastFilter         servicedto.UsageFilter
	filterCalls        int
	filterOptionCalls  int
	exportCalls        int
}

func (s *usageEventsStub) GetUsageOverview(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	return nil, nil
}

func (s *usageEventsStub) GetUsageOverviewRealtime(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	return nil, nil
}

func (s *usageEventsStub) ListUsageEvents(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	s.lastFilter = filter
	s.filterCalls++
	if s.eventsPage != nil {
		return s.eventsPage, s.err
	}
	return &servicedto.UsageEventsPage{Events: s.events, TotalCount: int64(len(s.events)), Page: 1, PageSize: servicedto.DefaultUsageEventsLimit, TotalPages: 1}, s.err
}

func (s *usageEventsStub) StreamUsageEvents(_ context.Context, filter servicedto.UsageFilter, emit func(servicedto.UsageEventRecord) error) error {
	s.lastFilter = filter
	s.exportCalls++
	events := s.events
	if s.exportEvents != nil {
		events = s.exportEvents
	}
	for _, event := range events {
		if err := emit(event); err != nil {
			return err
		}
	}
	return s.err
}

func (s *usageEventsStub) ListUsageEventFilterOptions(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	s.lastFilter = filter
	s.filterOptionCalls++
	if s.eventFilterOptions != nil {
		return s.eventFilterOptions, s.err
	}
	return &servicedto.UsageEventFilterOptions{}, s.err
}

func (s *usageEventsStub) GetAnalysis(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	return nil, s.err
}

type authCPAAPIKeyStub struct {
	row entities.CPAAPIKey
}

func (s *authCPAAPIKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func (s *authCPAAPIKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

type usageIdentitiesStub struct {
	items []entities.UsageIdentity
	err   error
}

func (s usageIdentitiesStub) ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentitiesPage(context.Context, service.ListUsageIdentitiesRequest) (service.ListUsageIdentitiesResponse, error) {
	return service.ListUsageIdentitiesResponse{Items: s.items, Total: int64(len(s.items))}, s.err
}

func (s usageIdentitiesStub) UpdateUsageIdentityAlias(context.Context, int64, string) (entities.UsageIdentity, error) {
	if len(s.items) == 0 {
		return entities.UsageIdentity{}, s.err
	}
	return s.items[0], s.err
}

func TestUsageEventsReturnsFilteredRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:                  42,
		Timestamp:           time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:               "claude-sonnet",
		ModelAlias:          "sonnet-business",
		ReasoningEffort:     "medium",
		ServiceTier:         "priority",
		ExecutorType:        "responses",
		Endpoint:            "POST /v1/responses",
		AuthType:            "apikey",
		Provider:            "OpenAI Mirror",
		Source:              "sk-provider-key",
		AuthIndex:           "2",
		Failed:              false,
		LatencyMS:           2045,
		TTFTMS:              usageEventInt64Ptr(45),
		InputTokens:         10,
		OutputTokens:        61,
		ReasoningTokens:     2,
		CachedTokens:        1,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         18,
		CostUSD:             0.1234,
		CostAvailable:       true,
		PricingStyle:        "claude",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
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
	if !contains(body, `"model_alias":"sonnet-business"`) {
		t.Fatalf("expected model alias in response body: %s", body)
	}
	if !contains(body, `"id":"42"`) || !contains(body, `"total_count":1`) || !contains(body, `"page":1`) || !contains(body, `"page_size":100`) || !contains(body, `"total_pages":1`) {
		t.Fatalf("expected pagination metadata and event id in response body: %s", body)
	}
	if !contains(body, `"source":"OpenAI Mirror"`) {
		t.Fatalf("expected resolved source display in response body: %s", body)
	}
	if contains(body, `sk-provider-key`) || contains(body, `sk-provider-prefix`) {
		t.Fatalf("expected raw source values to be redacted from response body: %s", body)
	}
	if contains(body, `"source_type"`) || contains(body, `"source_key"`) {
		t.Fatalf("expected source metadata fields to stay omitted, got %s", body)
	}
	if !contains(body, `"auth_index":"2"`) {
		t.Fatalf("expected auth index in response body: %s", body)
	}
	if !contains(body, `"timestamp":"2026-04-22T19:00:00+08:00"`) {
		t.Fatalf("expected project timezone timestamp in response body: %s", body)
	}
	if !contains(body, `"cache_read_tokens":3`) || !contains(body, `"cache_creation_tokens":4`) {
		t.Fatalf("expected cache token fields in response body: %s", body)
	}
	if !contains(body, `"reasoning_effort":"medium"`) {
		t.Fatalf("expected reasoning effort in response body: %s", body)
	}
	if !contains(body, `"service_tier":"priority"`) {
		t.Fatalf("expected service_tier in response body: %s", body)
	}
	if !contains(body, `"endpoint":"POST /v1/responses"`) {
		t.Fatalf("expected endpoint in response body: %s", body)
	}
	if !contains(body, `"ttft_ms":45`) {
		t.Fatalf("expected ttft_ms in response body: %s", body)
	}
	if !contains(body, `"speed_tps":29`) {
		t.Fatalf("expected speed_tps in response body: %s", body)
	}
	if !contains(body, `"executor_type":"responses"`) {
		t.Fatalf("expected executor_type in response body: %s", body)
	}
	if !contains(body, `"cost_usd":0.1234`) || !contains(body, `"cost_available":true`) || !contains(body, `"pricing_style":"claude"`) {
		t.Fatalf("expected backend cost fields in response body: %s", body)
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

func TestUsageEventsExportCSVReturnsFilteredRowsWithoutPagination(t *testing.T) {
	provider := &usageEventsStub{exportEvents: []servicedto.UsageEventRecord{{
		ID:                  52,
		Timestamp:           time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey:         "sk-export123456",
		Model:               "claude-sonnet",
		ModelAlias:          "sonnet-export",
		ReasoningEffort:     "medium",
		ServiceTier:         "priority",
		ExecutorType:        "responses",
		Endpoint:            "POST /v1/responses",
		AuthType:            "apikey",
		Provider:            "Provider Fallback",
		AuthIndex:           "authidx-export-main",
		Failed:              true,
		LatencyMS:           2045,
		TTFTMS:              usageEventInt64Ptr(45),
		InputTokens:         10,
		OutputTokens:        61,
		ReasoningTokens:     2,
		CachedTokens:        1,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         18,
		CostUSD:             0.1234,
		CostAvailable:       true,
		PricingStyle:        "claude",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{
		CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{
			ID:         7,
			APIKey:     "sk-export123456",
			DisplayKey: "sk-*********123456",
			KeyAlias:   "Export Key",
		}},
		UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
			ID:           12,
			Name:         "Export Provider",
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     "authidx-export-main",
			Type:         "openai",
			Provider:     "Provider",
		}}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/export?range=24h&page=3&page_size=100&model=claude-sonnet&source=authidx-export-main&result=failed&format=csv", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if provider.exportCalls != 1 || provider.filterCalls != 0 {
		t.Fatalf("expected export only, export=%d list=%d", provider.exportCalls, provider.filterCalls)
	}
	if provider.lastFilter.Page != 0 || provider.lastFilter.PageSize != 0 || provider.lastFilter.Limit != 0 || provider.lastFilter.Offset != 0 {
		t.Fatalf("expected export to drop pagination, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.AuthIndex != "authidx-export-main" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected export filters to match list filters, got %+v", provider.lastFilter)
	}
	if !contains(resp.Header().Get("Content-Type"), "text/csv") || !contains(resp.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("expected csv attachment headers, got content-type=%q disposition=%q", resp.Header().Get("Content-Type"), resp.Header().Get("Content-Disposition"))
	}
	if !regexp.MustCompile(`filename="usage-events-\d{8}-\d{6}\.csv"`).MatchString(resp.Header().Get("Content-Disposition")) {
		t.Fatalf("expected timestamped csv filename, got %q", resp.Header().Get("Content-Disposition"))
	}
	if !contains(body, "cpa_api_key_id") || !contains(body, "auth_index") || !contains(body, "model_alias") || !contains(body, "executor_type") || !contains(body, "is_identity_deleted") {
		t.Fatalf("expected cpa_api_key_id, auth_index, model_alias, executor_type, and is_identity_deleted columns, got %s", body)
	}
	if !regexp.MustCompile(`(?m)^id,timestamp,api_key,cpa_api_key_id,source,source_type,auth_index,is_identity_deleted,model,model_alias,reasoning_effort,`).MatchString(body) {
		t.Fatalf("expected model_alias to follow model in csv header, got %s", body)
	}
	if contains(body, "is_deleted") {
		t.Fatalf("expected export to use is_identity_deleted instead of is_deleted, got %s", body)
	}
	if contains(body, "cost_available") || contains(body, "pricing_style") {
		t.Fatalf("expected csv export to omit cost availability metadata, got %s", body)
	}
	if !contains(body, "Export Key") || !contains(body, ",7,") || !contains(body, "authidx-export-main") || !contains(body, "sonnet-export") || !contains(body, "responses") || !contains(body, "failed") {
		t.Fatalf("expected exported row values, got %s", body)
	}
}

func TestUsageEventsExportJSONIncludesAllExportFields(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:            53,
		Timestamp:     time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey:   "sk-json-export",
		Model:         "gpt-5",
		ModelAlias:    "gpt-json-alias",
		ServiceTier:   "default",
		ExecutorType:  "chat_completions",
		Endpoint:      "GET /v1/responses",
		AuthType:      "oauth",
		Source:        "claude-code",
		AuthIndex:     "auth-file-export",
		Failed:        false,
		LatencyMS:     300,
		InputTokens:   9,
		OutputTokens:  5,
		TotalTokens:   14,
		CostAvailable: true,
		PricingStyle:  "openai",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{
		CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{
			ID:       9,
			APIKey:   "sk-json-export",
			KeyAlias: "Team <Ops> & Co",
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/export?range=24h&format=json", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(resp.Header().Get("Content-Type"), "application/json") || !contains(resp.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("expected json attachment headers, got content-type=%q disposition=%q", resp.Header().Get("Content-Type"), resp.Header().Get("Content-Disposition"))
	}
	if !regexp.MustCompile(`filename="usage-events-\d{8}-\d{6}\.json"`).MatchString(resp.Header().Get("Content-Disposition")) {
		t.Fatalf("expected timestamped json filename, got %q", resp.Header().Get("Content-Disposition"))
	}
	if !contains(body, `"total_count":1`) || contains(body, `"page"`) || contains(body, `"page_size"`) {
		t.Fatalf("expected export metadata without pagination, got %s", body)
	}
	if !contains(body, `"auth_index":"auth-file-export"`) || !contains(body, `"model_alias":"gpt-json-alias"`) || !contains(body, `"executor_type":"chat_completions"`) || !contains(body, `"endpoint":"GET /v1/responses"`) || !contains(body, `"is_identity_deleted":true`) {
		t.Fatalf("expected raw export fields in json body, got %s", body)
	}
	if !contains(body, `"api_key":"Team <Ops> & Co"`) || contains(body, `\u003c`) || contains(body, `\u0026`) || contains(body, `\u003e`) {
		t.Fatalf("expected json export to preserve plain text values without HTML escaping, got %s", body)
	}
	if !contains(body, `"cpa_api_key_id":"9"`) || !contains(body, `"source_type":""`) || !contains(body, `"reasoning_effort":""`) || !contains(body, `"ttft_ms":null`) || !contains(body, `"speed_tps":null`) {
		t.Fatalf("expected json export to keep a stable field set, got %s", body)
	}
	if contains(body, `"is_deleted"`) {
		t.Fatalf("expected json export to use is_identity_deleted instead of is_deleted, got %s", body)
	}
	if contains(body, `"cost_available"`) || contains(body, `"pricing_style"`) {
		t.Fatalf("expected json export to omit cost availability metadata, got %s", body)
	}
}

func TestUsageEventsExportStreamSetupErrorReturnsServerError(t *testing.T) {
	for _, format := range []string{"csv", "json"} {
		t.Run(format, func(t *testing.T) {
			provider := &usageEventsStub{err: errors.New("stream setup failed")}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/export?range=24h&format="+format, nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500 before export body is written, got %d: %s", resp.Code, resp.Body.String())
			}
			if contains(resp.Body.String(), "usage-events-") || contains(resp.Body.String(), `"events":[`) || contains(resp.Body.String(), "id,timestamp") {
				t.Fatalf("expected error response instead of partial export body, got %s", resp.Body.String())
			}
		})
	}
}

func TestUsageEventsResponseDoesNotExposeSourceKey(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        48,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		AuthIndex: "provider-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:           12,
		Name:         "Provider Name",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "provider-auth-index",
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, `"source_key"`) {
		t.Fatalf("expected source_key to be removed from usage event response, got %s", body)
	}
}

func TestUsageEventsResolvesCPAAPIKeyAliasFromGroupKey(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:          49,
		Timestamp:   time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey: "sk-alpha123456",
		Model:       "claude-sonnet",
		AuthType:    "apikey",
		Provider:    "Fallback Provider",
	}}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{
		ID:         7,
		APIKey:     "sk-alpha123456",
		DisplayKey: "sk-*********123456",
		KeyAlias:   "Production Key",
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keyProvider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"api_key":"Production Key"`) {
		t.Fatalf("expected API key alias in response body: %s", body)
	}
	if contains(body, `sk-alpha123456`) || contains(body, `sk-*********123456`) {
		t.Fatalf("expected raw and masked key to be hidden when alias exists, got %s", body)
	}
}

func TestUsageEventsFallsBackToMaskedCPAAPIKeyFromGroupKey(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:          50,
		Timestamp:   time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey: "sk-beta654321",
		Model:       "claude-sonnet",
		AuthType:    "apikey",
		Provider:    "Fallback Provider",
	}}}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{
		ID:         8,
		APIKey:     "sk-beta654321",
		DisplayKey: "sk-*********654321",
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keyProvider})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"api_key":"sk-*********654321"`) {
		t.Fatalf("expected masked API key in response body: %s", body)
	}
	if contains(body, `sk-beta654321`) {
		t.Fatalf("expected raw API key to stay hidden, got %s", body)
	}
}

func TestUsageEventsFallsBackToCanonicalMaskedAPIKeyWhenGroupKeyIsUnmatched(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:          51,
		Timestamp:   time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey: "sk-BabcdefghijklmnopqrstuvwxyzmaWyTA",
		Model:       "claude-sonnet",
		AuthType:    "apikey",
		Provider:    "Fallback Provider",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"api_key":"sk-*********maWyTA"`) {
		t.Fatalf("expected canonical masked API key in response body: %s", body)
	}
	if contains(body, `sk-BabcdefghijklmnopqrstuvwxyzmaWyTA`) || contains(body, `sk-B***************************WyTA`) {
		t.Fatalf("expected raw and variable-length masked keys to stay hidden, got %s", body)
	}
}

func TestUsageEventsResolvesAPIKeySourceFromProviderIdentity(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        44,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		Source:    "sk-provider-key",
		AuthIndex: "provider-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:            12,
		Name:          "Provider Name",
		Prefix:        "Team Prefix",
		AuthType:      entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName:  "apikey",
		Identity:      "provider-auth-index",
		Type:          "openai",
		Provider:      "Provider",
		TotalRequests: 1,
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"source":"Team Prefix"`) {
		t.Fatalf("expected source to use provider identity displayName, got %s", body)
	}
	if !contains(body, `"source_type":"openai"`) {
		t.Fatalf("expected source_type to use provider identity type, got %s", body)
	}
	if contains(body, `"source_key"`) {
		t.Fatalf("expected source_key to stay omitted, got %s", body)
	}
	if contains(body, `Fallback Provider`) || contains(body, `sk-provider-key`) {
		t.Fatalf("expected fallback and raw source to be hidden, got %s", body)
	}
}

func TestUsageEventsDoesNotResolveProviderIdentityFromSource(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        45,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		Source:    "provider-auth-index",
		AuthIndex: "missing-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:            12,
		Name:          "Provider Name",
		Prefix:        "Team Prefix",
		AuthType:      entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName:  "apikey",
		Identity:      "provider-auth-index",
		Type:          "openai",
		Provider:      "Provider",
		TotalRequests: 1,
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, `"source":"Team Prefix"`) || contains(body, `"source_key"`) {
		t.Fatalf("expected event source not to resolve identity through usage event source, got %s", body)
	}
	if !contains(body, `"source":"Fallback Provider"`) {
		t.Fatalf("expected auth_index fallback when identity is missing, got %s", body)
	}
}

func TestUsageEventsMarksRowDeletedWhenAuthIndexHasNoIdentity(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        46,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		AuthIndex: "missing-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:           12,
		Name:         "Provider Name",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "other-auth-index",
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"isDelete":true`) {
		t.Fatalf("expected missing identity row to be marked deleted, got %s", body)
	}
}

func TestUsageEventsDoesNotMarkRowDeletedWhenAuthIndexMatchesIdentity(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        47,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		AuthIndex: "provider-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:           12,
		Name:         "Provider Name",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "provider-auth-index",
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, `"isDelete":true`) {
		t.Fatalf("expected matched identity row not to be marked deleted, got %s", body)
	}
}

func TestUsageEventsKeepsFallbackSourceWhenAuthIndexIsMissing(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        43,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "OpenAI Mirror",
		Source:    "sk-provider-key",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"source":"OpenAI Mirror"`) || contains(body, `"source_key"`) {
		t.Fatalf("expected provider source fallback without source_key, got %s", body)
	}
}

func TestUsageEventsPassesPaginationAndAuthIndexSourceFilter(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{}, TotalCount: 0, Page: 3, PageSize: 100, TotalPages: 0}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h&page=3&page_size=100&model=claude-sonnet&source=authidx-openai-main&result=failed", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.Page != 3 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 200 {
		t.Fatalf("expected pagination filter, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.AuthIndex != "authidx-openai-main" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected source filter to be translated to auth_index only, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if !contains(body, `"page":3`) || !contains(body, `"page_size":100`) || !contains(body, `"total_count":0`) || !contains(body, `"total_pages":0`) {
		t.Fatalf("expected response pagination metadata, got %s", body)
	}
}

func TestUsageEventsPassesAuthFileIdentitySourceFilterAsAuthIndex(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{}, TotalCount: 0, Page: 1, PageSize: 100, TotalPages: 0}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h&source=auth-file-index", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.AuthIndex != "auth-file-index" || provider.lastFilter.Source != "" {
		t.Fatalf("expected auth file identity source filter to use auth_index only, got %+v", provider.lastFilter)
	}
}

func TestUsageEventsDoesNotReturnFilterOptions(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{
		Events: []servicedto.UsageEventRecord{{
			ID: 7, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC), Model: "gpt-5", AuthType: "apikey", Provider: "Provider A", Source: "source-a", Failed: true,
		}},
		TotalCount: 2, Page: 1, PageSize: 20, TotalPages: 1,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if contains(body, `"models":`) || contains(body, `"sources":`) {
		t.Fatalf("expected events response to omit filter options, got %s", body)
	}
}

func TestUsageEventModelFilterOptionsReturnsStableModels(t *testing.T) {
	provider := &usageEventsStub{eventFilterOptions: &servicedto.UsageEventFilterOptions{
		Models: []string{"claude-sonnet", "gpt-5"},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/filters/models?range=24h&model=ignored&source=ignored&result=failed&page=3&page_size=20", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.filterOptionCalls != 1 || provider.filterCalls != 0 {
		t.Fatalf("expected model filter options endpoint only, events=%d filterOptions=%d", provider.filterCalls, provider.filterOptionCalls)
	}
	if provider.lastFilter.Range != "" || provider.lastFilter.StartTime != nil || provider.lastFilter.EndTime != nil || provider.lastFilter.Model != "" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "" || provider.lastFilter.Page != 0 || provider.lastFilter.PageSize != 0 {
		t.Fatalf("expected model filters endpoint to ignore query filters, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if body != `{"models":["claude-sonnet","gpt-5"]}` {
		t.Fatalf("expected stable model filter options, got %s", body)
	}
}

func TestUsageEventSpeedTPS(t *testing.T) {
	tests := []struct {
		name string
		row  servicedto.UsageEventRecord
		want *float64
	}{
		{
			name: "uses output tokens after first token over generation duration",
			row: servicedto.UsageEventRecord{
				LatencyMS:    2045,
				TTFTMS:       usageEventInt64Ptr(45),
				OutputTokens: 61,
			},
			want: usageEventFloat64Ptr(30),
		},
		{
			name: "uses visible output tokens after first token over generation duration",
			row: servicedto.UsageEventRecord{
				LatencyMS:       2045,
				TTFTMS:          usageEventInt64Ptr(45),
				OutputTokens:    61,
				ReasoningTokens: 2,
			},
			want: usageEventFloat64Ptr(29),
		},
		{
			name: "omits speed without ttft",
			row: servicedto.UsageEventRecord{
				LatencyMS:    2045,
				OutputTokens: 61,
			},
		},
		{
			name: "omits speed when latency does not exceed ttft",
			row: servicedto.UsageEventRecord{
				LatencyMS:    45,
				TTFTMS:       usageEventInt64Ptr(45),
				OutputTokens: 61,
			},
		},
		{
			name: "omits speed when only first token is present",
			row: servicedto.UsageEventRecord{
				LatencyMS:    2045,
				TTFTMS:       usageEventInt64Ptr(45),
				OutputTokens: 1,
			},
		},
		{
			name: "omits speed when only first visible token is present",
			row: servicedto.UsageEventRecord{
				LatencyMS:       2045,
				TTFTMS:          usageEventInt64Ptr(45),
				OutputTokens:    4,
				ReasoningTokens: 3,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := usageEventSpeedTPS(tc.row)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil speed, got %v", *got)
				}
				return
			}
			if got == nil || math.Abs(*got-*tc.want) > 0.000001 {
				t.Fatalf("expected speed %.6f, got %v", *tc.want, got)
			}
		})
	}
}

func TestUsageEventSourceFilterOptionsReturnsIdentitySources(t *testing.T) {
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{ID: 1, Name: "Claude Main", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-a", Type: "openai", Provider: "Provider A", TotalRequests: 3}, {ID: 2, Name: "Provider A", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-b", Type: "openai", Provider: "Provider A"}, {ID: 3, Name: "Auth User", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-1", Type: "claude", Provider: "Claude", TotalRequests: 2}, {ID: 4, Name: "Zero Request User", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-zero", Type: "claude", Provider: "Claude"}, {ID: 5, Name: "Zero Provider", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-zero", Type: "openai", Provider: "Zero Provider"}, {ID: 6, Name: "Deleted Source", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-deleted", Type: "openai", Provider: "Deleted Provider", TotalRequests: 5, IsDeleted: true}, {ID: 7, Name: "   ", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-display", Type: "claude", Provider: "Claude Display", TotalRequests: 1}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/filters/sources?range=24h&model=ignored&source=ignored&result=failed&page=3&page_size=20", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.filterOptionCalls != 0 || provider.filterCalls != 0 {
		t.Fatalf("expected source filter options endpoint to use identities only, events=%d filterOptions=%d", provider.filterCalls, provider.filterOptionCalls)
	}
	body := resp.Body.String()
	if !contains(body, `"sources":[`) || !contains(body, `"value":"authidx-source-a"`) || !contains(body, `"label":"Claude Main"`) || !contains(body, `"displayName":"Claude Main"`) || !contains(body, `"value":"auth-1"`) || !contains(body, `"label":"Auth User"`) || !contains(body, `"value":"auth-display"`) || !contains(body, `"label":"Claude Display"`) || !contains(body, `"displayName":"Claude Display"`) {
		t.Fatalf("expected stable identity source filter options with display names, got %s", body)
	}
	if contains(body, `"models"`) {
		t.Fatalf("expected source filter options endpoint not to return models, got %s", body)
	}
	if contains(body, `"value":"auth:auth-1"`) || contains(body, `"value":"provider:Provider A"`) || contains(body, `"value":"provider:1"`) || contains(body, `"value":"provider:2"`) {
		t.Fatalf("expected source filter values without prefixes, got %s", body)
	}
	if contains(body, `Zero Request User`) || contains(body, `Zero Provider`) || contains(body, `auth-zero`) || contains(body, `authidx-source-zero`) {
		t.Fatalf("expected zero-request source filter options to be omitted, got %s", body)
	}
	if contains(body, `Deleted Source`) || contains(body, `Deleted Provider`) || contains(body, `authidx-deleted`) {
		t.Fatalf("expected deleted source filter options to be omitted, got %s", body)
	}
}

func usageEventInt64Ptr(value int64) *int64 {
	return &value
}

func usageEventFloat64Ptr(value float64) *float64 {
	return &value
}

//go:linkname usageEventSpeedTPS cpa-usage-keeper/internal/api.usageEventSpeedTPS
func usageEventSpeedTPS(row servicedto.UsageEventRecord) *float64

func contains(s string, sub string) bool {
	return strings.Contains(s, sub)
}
