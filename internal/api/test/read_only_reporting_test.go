package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type reportingKeysStub struct{ authLoginKeyStub }

func (s *reportingKeysStub) ListReportingAPIKeys(context.Context) ([]service.ReportingAPIKey, error) {
	return []service.ReportingAPIKey{{ID: "1", APIKey: "secret-current"}, {ID: "2", APIKey: "secret-retired", Historical: true}, {ID: "historical:123456789012345678901234", APIKey: "secret-orphan", Historical: true}}, nil
}

type reportingIdentityStub struct{ identityMetadataStub }

func (s reportingIdentityStub) ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, nil
}
func (s reportingIdentityStub) GetUsageIdentity(context.Context, int64) (service.UsageIdentityDetail, error) {
	return service.UsageIdentityDetail{Identity: s.items[0]}, nil
}

type reportingUsageStub struct{ readOnlyUsageStub }

func (s *reportingUsageStub) ListUsageEvents(_ context.Context, f servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	s.filter = f
	return &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{{ID: 7, Timestamp: time.Now(), APIGroupKey: "secret-retired", AuthIndex: "secret-auth-index", Source: "secret-source", ClientIP: new("secret-ip"), UserAgent: new("secret-agent"), RequestID: "secret-request", Model: "gpt-model", InputTokens: 12, TotalTokens: 19, CostUSD: 0.125, CostAvailable: true, Failed: true}}, Page: 1, PageSize: 50, TotalCount: 1}, nil
}
func (s *reportingUsageStub) ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	return &servicedto.UsageEventFilterOptions{Models: []string{"gpt-model"}}, nil
}
func (s *reportingUsageStub) StreamUsageEvents(ctx context.Context, f servicedto.UsageFilter, emit func(servicedto.UsageEventRecord) error) error {
	page, _ := s.ListUsageEvents(ctx, f)
	return emit(page.Events[0])
}

type reportingRankingStub struct{}

func (reportingRankingStub) Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	return ranking.Leaderboard{Entries: []ranking.LeaderboardEntry{{ParticipantID: "2", DisplayName: "secret-ranking-name", KeyAlias: "secret-alias", Value: 19}}}, nil
}
func (reportingRankingStub) UpdateProfile(context.Context, int64, string, uint8) (ranking.LocalProfile, error) {
	panic("reporting invoked a write")
}

func TestReadOnlyReportingSanitizationScopeAndPermissions(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	cfg := AuthConfig{Enabled: true, LoginPassword: "admin-password-long", ReadOnlyPassword: "read-password-long"}
	usage := &reportingUsageStub{}
	keys := &reportingKeysStub{}
	row := entities.UsageIdentity{ID: 3, Identity: "secret-auth-index", Name: "secret-name", Alias: new("secret-alias"), FileName: new("secret-file"), FilePath: new("secret-path"), Note: new("secret-note"), LookupKey: "secret-lookup", BaseURL: "secret-url", AccountID: new("secret-account"), Provider: "secret-provider", Prefix: "secret-prefix", Type: "secret-custom-type", AuthType: 1, TotalRequests: 17, TotalTokens: 190}
	identities := reportingIdentityStub{identityMetadataStub{items: []entities.UsageIdentity{row}, pagedActiveItems: []entities.UsageIdentity{row}, pagedActiveTotal: 1}}
	router := NewRouter(nil, nil, usage, nil, cfg, NewAuthHandler(cfg, sessions), "", OptionalProviders{CPAAPIKeys: keys, UsageIdentity: identities, Quota: &quotaProviderStub{}, LocalRanking: reportingRankingStub{}})
	login := serveCredentialMutation(router, "POST", "/api/v1/auth/read-only-login", `{"password":"read-password-long"}`)
	if login.Code != 204 {
		t.Fatal(login.Code)
	}
	cookie := login.Result().Cookies()[0]
	viewer, _, _ := sessions.CreateAPIKeyViewer(1)
	viewerCookie := &http.Cookie{Name: cookie.Name, Value: viewer}
	routes := []string{"keys", "events?range=today", "events/filters?range=today", "events/export?range=today&format=json", "events/export?range=today&format=csv", "accounts?page=1&page_size=10", "accounts/3", "accounts/quota-cache", "ranking/local/leaderboards?period=today&metric=total_tokens"}
	for _, route := range routes {
		path := "/api/v1/read-only/" + route
		response := serveAPIGet(router, path, cookie)
		if response.Code != 200 {
			t.Fatalf("%s returned %d: %s", route, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "secret-") {
			t.Fatalf("%s disclosed credential/client metadata", route)
		}
		if serveAPIGet(router, path).Code != 401 || serveAPIGet(router, path, viewerCookie).Code != 403 {
			t.Fatalf("role isolation failed: %s", route)
		}
		if route == "events?range=today" {
			var payload struct {
				Events []struct {
					APIKey string `json:"api_key"`
					Failed bool
					Tokens struct {
						TotalTokens int64 `json:"total_tokens"`
					}
				}
			}
			if json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Events) != 1 || payload.Events[0].APIKey != "Historical key 2" || !payload.Events[0].Failed || payload.Events[0].Tokens.TotalTokens != 19 {
				t.Fatal("reporting metrics lost")
			}
		}
	}
	for _, id := range []string{"2", "historical:123456789012345678901234"} {
		response := serveAPIGet(router, "/api/v1/read-only/key-overview?range=today&api_key_id="+id, cookie)
		if response.Code != 200 || usage.filter.ReportingAPIGroupKey == "" {
			t.Fatalf("historical selector failed: %s", id)
		}
	}
	if serveAPIGet(router, "/api/v1/read-only/events?range=today&source=secret-auth-index", cookie).Code != 404 {
		t.Fatal("raw credential selector accepted")
	}
	if serveAPIGet(router, "/api/v1/read-only/events?range=today&api_key_id=999", cookie).Code != 404 {
		t.Fatal("unknown key accepted")
	}
	if serveAPIGet(router, "/api/v1/read-only/events?range=today&api_key_id=bad", cookie).Code != 400 {
		t.Fatal("invalid key accepted")
	}
	if serveAPIGet(router, "/api/v1/read-only/events/7/request-log", cookie).Code != 404 {
		t.Fatal("raw log route exposed")
	}
	for _, route := range []string{"events", "accounts/3", "accounts/quota-cache", "ranking/local/profiles/2"} {
		if response := serveCredentialMutation(router, "POST", "/api/v1/read-only/"+route, `{}`); response.Code != 404 {
			t.Fatal("reporting write route exposed")
		}
	}
}
