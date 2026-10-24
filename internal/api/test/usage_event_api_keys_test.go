package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageEventAPIKeyOptionsStub struct {
	usageEventsStub
	keys  []entities.CPAAPIKey
	calls int
}

func (s *usageEventAPIKeyOptionsStub) ListUsageEventAPIKeyFilterOptions(_ context.Context, filter servicedto.UsageFilter) ([]entities.CPAAPIKey, error) {
	s.lastFilter = filter
	s.calls++
	return s.keys, s.err
}

func TestUsageEventAPIKeyOptionsParseEventFiltersAndReturnSafeFreshOptions(t *testing.T) {
	provider := &usageEventAPIKeyOptionsStub{keys: []entities.CPAAPIKey{
		{ID: 9007199254740993, APIKey: "sk-alpha123456", KeyAlias: "Team Key"},
		{ID: 2, APIKey: "sk-beta654321"},
	}}
	router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "")
	path := "/api/v1/usage/events/filters/api-keys?range=24h&model=gpt-5&source=auth-1&auth_type=1&result=failed&api_key_id=invalid&page=bad&page_size=bad&cursor=bad&cursor_mode=bad"
	resp := serveAPIGet(router, path)
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	assertNoStoreHeaders(t, resp)
	want := `{"options":[{"id":"9007199254740993","label":"Team Key"},{"id":"2","label":"sk-*********654321"}]}`
	if resp.Body.String() != want {
		t.Fatalf("expected safe ID/label response, got %s", resp.Body.String())
	}
	filter := provider.lastFilter
	if filter.Range != "24h" || filter.StartTime == nil || filter.EndTime == nil || filter.Model != "gpt-5" || filter.Source != "" || filter.AuthIndex != "auth-1" || filter.AuthType != "oauth" || filter.Result != "failed" {
		t.Fatalf("unexpected event dimensions: %+v", filter)
	}
	if filter.APIKeyID != "" || filter.Page != 0 || filter.PageSize != 0 || filter.Limit != 0 || filter.Offset != 0 || filter.CursorMode || filter.CursorTimestamp != nil {
		t.Fatalf("key or pagination constrained candidates: %+v", filter)
	}
	provider.keys = nil
	resp = serveAPIGet(router, path)
	if provider.calls != 2 || resp.Body.String() != `{"options":[]}` {
		t.Fatalf("expected provider refreshed on every request, calls=%d body=%s", provider.calls, resp.Body.String())
	}
}

func TestUsageEventAPIKeyOptionsRejectInvalidEventFilters(t *testing.T) {
	for _, query := range []string{"range=bad", "range=24h&result=bad", "range=24h&auth_type=bad"} {
		t.Run(query, func(t *testing.T) {
			provider := &usageEventAPIKeyOptionsStub{}
			router := api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "")
			resp := serveAPIGet(router, "/api/v1/usage/events/filters/api-keys?"+query)
			if resp.Code != http.StatusBadRequest || provider.calls != 0 {
				t.Fatalf("expected rejected filter before lookup, status=%d calls=%d", resp.Code, provider.calls)
			}
		})
	}
}

func TestUsageEventAPIKeyOptionsHandleUnavailableProviderAndQueryErrors(t *testing.T) {
	router := api.NewRouter(nil, nil, nil, nil, api.AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events/filters/api-keys?range=24h")
	if resp.Code != http.StatusOK || resp.Body.String() != `{"options":[]}` {
		t.Fatalf("expected empty options without provider, status=%d body=%s", resp.Code, resp.Body.String())
	}
	provider := &usageEventAPIKeyOptionsStub{usageEventsStub: usageEventsStub{err: errors.New("database lookup failed")}}
	router = api.NewRouter(nil, nil, provider, nil, api.AuthConfig{}, nil, "")
	resp = serveAPIGet(router, "/api/v1/usage/events/filters/api-keys?range=24h")
	if resp.Code != http.StatusInternalServerError || strings.Contains(resp.Body.String(), "database lookup failed") {
		t.Fatalf("expected sanitized lookup error, status=%d body=%s", resp.Code, resp.Body.String())
	}
}
