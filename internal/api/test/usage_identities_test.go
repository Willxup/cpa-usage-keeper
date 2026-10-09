package test

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

type identityMetadataStub struct {
	service.UsageIdentityProvider
	items            []entities.UsageIdentity
	activeItems      []entities.UsageIdentity
	pagedActiveItems []entities.UsageIdentity
	pagedActiveTotal int64
	pagedTypeCounts  []service.UsageIdentityTypeCount
	pagedHealth      []service.UsageCredentialHealthSnapshot
	pagedActiveReq   *service.ListUsageIdentitiesRequest
}

func (s identityMetadataStub) ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	if s.activeItems != nil {
		return s.activeItems, nil
	}
	return s.items, nil
}

func (s identityMetadataStub) ListActiveUsageIdentitiesPage(_ context.Context, request service.ListUsageIdentitiesRequest) (service.ListUsageIdentitiesResponse, error) {
	if s.pagedActiveReq != nil {
		*s.pagedActiveReq = request
	}
	return service.ListUsageIdentitiesResponse{Items: s.pagedActiveItems, Total: s.pagedActiveTotal, TypeCounts: s.pagedTypeCounts, CredentialHealth: s.pagedHealth}, nil
}

func TestUsageIdentitiesRouteReturnsMetadataStatsAndActiveRows(t *testing.T) {
	firstUsedAt := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	lastUsedAt := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	statsUpdatedAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)
	deletedAt := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)

	activeIdentity := entities.UsageIdentity{
		ID:                         1,
		Name:                       "Claude Desktop",
		AuthType:                   entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName:               "oauth",
		Identity:                   "2",
		Type:                       "auth-file",
		Provider:                   "anthropic",
		Prefix:                     "claude-team",
		Priority:                   new(4),
		Disabled:                   new(true),
		Note:                       new("desktop note"),
		TotalRequests:              10,
		SuccessCount:               8,
		FailureCount:               2,
		InputTokens:                100,
		OutputTokens:               200,
		ReasoningTokens:            30,
		CachedTokens:               40,
		CacheReadTokens:            45,
		TotalTokens:                370,
		LastAggregatedUsageEventID: 99,
		FirstUsedAt:                &firstUsedAt,
		LastUsedAt:                 &lastUsedAt,
		StatsUpdatedAt:             &statsUpdatedAt,
		CreatedAt:                  createdAt,
		UpdatedAt:                  updatedAt,
	}
	deletedIdentity := entities.UsageIdentity{
		ID:           2,
		Name:         "Deleted Provider",
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "sk-deleted-provider-secret",
		Type:         "openai",
		Provider:     "OpenAI",
		IsDeleted:    true,
		DeletedAt:    &deletedAt,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{
		items:       []entities.UsageIdentity{activeIdentity, deletedIdentity},
		activeItems: []entities.UsageIdentity{activeIdentity},
	}})
	resp := serveAPIGet(router, "/api/v1/usage/identities")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !strings.Contains(body, `"identities":[`) || !strings.Contains(body, `"id":"1"`) || !strings.Contains(body, `"identity":"2"`) {
		t.Fatalf("expected auth file identity row in response, got %s", body)
	}
	if strings.Contains(body, "Deleted Provider") || strings.Contains(body, "sk-deleted-provider-secret") || strings.Contains(body, `"deleted_at"`) {
		t.Fatalf("expected deleted identities to be filtered from response, got %s", body)
	}
	for _, expected := range []string{
		`"name":"Claude Desktop"`,
		`"auth_type":1`,
		`"auth_type_name":"oauth"`,
		`"type":"auth-file"`,
		`"provider":"anthropic"`,
		`"prefix":"claude-team"`,
		`"priority":4`,
		`"disabled":true`,
		`"note":"desktop note"`,
		`"total_requests":10`,
		`"success_count":8`,
		`"failure_count":2`,
		`"input_tokens":100`,
		`"output_tokens":200`,
		`"reasoning_tokens":30`,
		`"cache_read_tokens":45`,
		`"total_tokens":370`,
		`"last_aggregated_usage_event_id":"99"`,
		`"first_used_at":"2026-05-04T08:00:00Z"`,
		`"last_used_at":"2026-05-04T09:00:00Z"`,
		`"stats_updated_at":"2026-05-04T10:00:00Z"`,
		`"is_deleted":false`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
	if strings.Contains(body, `"cached_tokens"`) {
		t.Fatalf("did not expect legacy cached_tokens in response body: %s", body)
	}
}

func TestUsageIdentitiesRouteReturnsPublishedMetadataFields(t *testing.T) {
	activeStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	activeUntil := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	accountID := "acct_123"
	planType := "team"
	baseURL := "https://api.openai.com/v1"
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{items: []entities.UsageIdentity{{
		ID:           1,
		Name:         "Codex Account",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "codex-auth",
		Type:         "codex",
		Provider:     "Codex",
		Prefix:       "codex-prefix",
		BaseURL:      baseURL,
		Priority:     new(1),
		Disabled:     nil,
		Note:         new("codex note"),
		AccountID:    &accountID,
		ActiveStart:  &activeStart,
		ActiveUntil:  &activeUntil,
		PlanType:     &planType,
	}}}})
	resp := serveAPIGet(router, "/api/v1/usage/identities")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	for _, expected := range []string{
		`"subscription":{"provider":"codex","plan":"team"}`,
		`"active_start":"2026-05-01T00:00:00Z"`,
		`"active_until":"2026-06-01T00:00:00Z"`,
		`"prefix":"codex-prefix"`,
		`"priority":1`,
		`"disabled":false`,
		`"note":"codex note"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected API response to include %s, got %s", expected, body)
		}
	}
	for _, forbidden := range []string{
		`"base_url"`,
		`"account_id"`,
		`"plan_type"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("expected API response not to include %s, got %s", forbidden, body)
		}
	}
}

func TestUsageIdentitiesPageRouteFiltersByAuthTypeAndPaginates(t *testing.T) {
	captured := service.ListUsageIdentitiesRequest{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{
		pagedActiveReq:   &captured,
		pagedActiveTotal: 25,
		pagedActiveItems: []entities.UsageIdentity{{
			ID:           11,
			Name:         "Codex Account",
			AuthType:     entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "oauth",
			Identity:     "codex-auth",
			Type:         "codex",
			Provider:     "Codex",
			FileName:     new("codex-user.json"),
			FilePath:     new("/data/auths/codex-user.json"),
		}},
	}})
	resp := serveAPIGet(router, "/api/v1/usage/identities/page?auth_type=1&page=2&page_size=10&active_only=true&sort=priority")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if captured.AuthType == nil || *captured.AuthType != entities.UsageIdentityAuthTypeAuthFile || captured.Page != 2 || captured.PageSize != 10 || captured.ActiveOnly == nil || !*captured.ActiveOnly || captured.Sort != "priority" {
		t.Fatalf("expected auth_type/page/page_size/active_only/sort request, got %+v", captured)
	}
	for _, expected := range []string{`"identities":[`, `"id":"11"`, `"file_name":"codex-user.json"`, `"file_path":"/data/auths/codex-user.json"`, `"total_count":25`, `"page":2`, `"page_size":10`, `"total_pages":3`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesPageRouteAcceptsRepeatedTypesAndReturnsTypeCounts(t *testing.T) {
	captured := service.ListUsageIdentitiesRequest{}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{
		pagedActiveReq:   &captured,
		pagedActiveTotal: 3,
		pagedTypeCounts: []service.UsageIdentityTypeCount{
			{Type: "claude", Count: 2},
			{Type: "anthropic", Count: 1},
			{Type: "openai", Count: 4},
		},
		pagedActiveItems: []entities.UsageIdentity{{
			ID:           12,
			Name:         "Claude Team",
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     "claude-auth",
			Type:         "claude",
			Provider:     "Claude Team",
		}},
	}})
	resp := serveAPIGet(router, "/api/v1/usage/identities/page?auth_type=2&type=claude&type=%20openai%20&type=&page=1&page_size=10")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if captured.AuthType == nil || *captured.AuthType != entities.UsageIdentityAuthTypeAIProvider || !reflect.DeepEqual(captured.Types, []string{"claude", " openai "}) {
		t.Fatalf("expected auth_type and repeated type filters, got %+v", captured)
	}
	for _, expected := range []string{`"type_counts":[`, `"type":"claude"`, `"count":2`, `"type":"anthropic"`, `"count":1`, `"type":"openai"`, `"count":4`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesPageRouteReturnsCredentialHealthSnapshot(t *testing.T) {
	windowStart := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 6, 15, 13, 0, 0, 0, time.UTC)
	bucketStart := time.Date(2026, 6, 15, 12, 40, 0, 0, time.UTC)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{
		pagedActiveTotal: 1,
		pagedActiveItems: []entities.UsageIdentity{{
			ID:           12,
			Name:         "Claude Team",
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     "claude-auth",
			Type:         "claude",
			Provider:     "Claude Team",
		}},
		pagedHealth: []service.UsageCredentialHealthSnapshot{{
			WindowSeconds:   5 * 60 * 60,
			BucketSeconds:   10 * 60,
			WindowStart:     windowStart,
			WindowEnd:       windowEnd,
			TotalSuccess:    2,
			TotalFailure:    1,
			SuccessRate:     66.6666666667,
			InputTokens:     400,
			CacheReadTokens: 250,
			Buckets: []service.UsageCredentialHealthBucket{{
				StartTime: bucketStart,
				EndTime:   bucketStart.Add(10 * time.Minute),
				Success:   2,
				Failure:   1,
				Rate:      0.6666666667,
			}},
		}},
	}})
	resp := serveAPIGet(router, "/api/v1/usage/identities/page?auth_type=2&page=1&page_size=10")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	for _, expected := range []string{
		`"credential_health":{`,
		`"window_seconds":18000`,
		`"bucket_seconds":600`,
		`"window_start":"2026-06-15T08:00:00Z"`,
		`"window_end":"2026-06-15T13:00:00Z"`,
		`"total_success":2`,
		`"total_failure":1`,
		`"success_rate":66.6666666667`,
		`"input_tokens":400`,
		`"cache_read_tokens":250`,
		`"buckets":[{"start_time":"2026-06-15T12:40:00Z","end_time":"2026-06-15T12:50:00Z","success":2,"failure":1,"rate":0.6666666667}]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in response body: %s", expected, body)
		}
	}
}

func TestUsageIdentitiesRoutePublishesAIProviderAuthIndexWithoutLookupKey(t *testing.T) {
	authIndex := "provider-auth-index"
	lookupKey := "sk-live-secret-value"
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{items: []entities.UsageIdentity{
		{ID: 1, Name: "Provider Name", Prefix: "Team Prefix", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: authIndex, LookupKey: lookupKey, Type: "openai", Provider: "OpenAI", FileName: new("should-not-return.json"), FilePath: new("/data/auths/should-not-return.json")},
	}}})
	resp := serveAPIGet(router, "/api/v1/usage/identities")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	for _, hidden := range []string{lookupKey, `"file_name"`, `"file_path"`, "should-not-return.json"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("AI provider response leaked %q: %s", hidden, body)
		}
	}
	if !strings.Contains(body, `"prefix":"Team Prefix"`) {
		t.Fatalf("missing published prefix: %s", body)
	}
	if !strings.Contains(body, `"identity":"`+authIndex+`"`) {
		t.Fatalf("expected AI provider auth-index %q in response body: %s", authIndex, body)
	}
	if !strings.Contains(body, `"name":"Provider Name"`) || !strings.Contains(body, `"provider":"OpenAI"`) || !strings.Contains(body, `"displayName":"Team Prefix"`) {
		t.Fatalf("expected AI provider display fields to use usage_identities values directly, got %s", body)
	}
}

func TestUsageIdentityReplacesLegacyMetadataRoutes(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: identityMetadataStub{}})
	for _, path := range []string{"/api/v1/auth-files", "/api/v1/provider-metadata"} {
		resp := serveAPIGet(router, path)

		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d: %s", path, resp.Code, resp.Body.String())
		}
	}
}
