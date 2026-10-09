package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestUsageIdentityStatsResetPreservesLifetimeAndCountsLaterAggregation(t *testing.T) {
	for _, authType := range []entities.UsageIdentityAuthType{entities.UsageIdentityAuthTypeAuthFile, entities.UsageIdentityAuthTypeAIProvider} {
		t.Run(fmt.Sprint(authType), func(t *testing.T) {
			db := openAPITestDatabase(t)
			ctx := context.Background()
			now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
			identity := entities.UsageIdentity{ID: 1, AuthType: authType, Identity: "reset-fixture", Name: "Fixture", Type: "codex", TotalRequests: 10, SuccessCount: 8, FailureCount: 2, TotalTokens: 1000, InputTokens: 600, CacheReadTokens: 300, LastAggregatedUsageEventID: 5, LastUsedAt: &now}
			if err := db.Create(&identity).Error; err != nil {
				t.Fatal(err)
			}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
			reset := func() map[string]any {
				t.Helper()
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/v1/usage/identities/1/stats/reset", nil)
				request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
				router.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("reset status %d: %s", response.Code, response.Body.String())
				}
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				return body
			}
			body := reset()
			if body["total_requests"] != float64(10) || body["stats_reset_at"] == nil {
				t.Fatalf("lost lifetime/reset time: %+v", body)
			}
			for field, value := range body["period_stats"].(map[string]any) {
				if value != float64(0) {
					t.Fatalf("initial period %s=%v", field, value)
				}
			}
			stored, err := repository.FindUsageIdentityByID(ctx, db, 1)
			if err != nil {
				t.Fatal(err)
			}
			if stored.TotalTokens != 1000 || stored.LastAggregatedUsageEventID != 5 || !stored.LastUsedAt.Equal(now) {
				t.Fatalf("reset changed lifetime/cursor/history: %+v", stored)
			}
			eventAuthType := "oauth"
			if authType == entities.UsageIdentityAuthTypeAIProvider {
				eventAuthType = "apikey"
			}
			// 即使请求时间早于重置，尚未聚合的事件仍进入新一轮。
			event := entities.UsageEvent{ID: 6, EventKey: "reset-event", AuthType: eventAuthType, AuthIndex: identity.Identity, Timestamp: now.Add(-time.Hour), TotalTokens: 120, InputTokens: 80, CacheReadTokens: 20}
			if err := db.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.AggregateUsageIdentityStats(ctx, db, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/page", nil))
			var page struct {
				Identities []struct {
					TotalRequests int64            `json:"total_requests"`
					Period        map[string]int64 `json:"period_stats"`
				} `json:"identities"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Identities) != 1 {
				t.Fatalf("page: %s", response.Body.String())
			}
			current := page.Identities[0]
			if current.TotalRequests != 11 || current.Period["total_requests"] != 1 || current.Period["success_count"] != 1 || current.Period["total_tokens"] != 120 || current.Period["input_tokens"] != 80 || current.Period["cache_read_tokens"] != 20 {
				t.Fatalf("bad period/lifetime: %+v", current)
			}
			body = reset()
			if body["total_requests"] != float64(11) || body["period_stats"].(map[string]any)["total_requests"] != float64(0) {
				t.Fatalf("second reset: %+v", body)
			}
			if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{Identity: identity.Identity, Name: "Renamed", Type: "codex"}}, authType, now.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/page", nil))
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.Identities[0].Period["total_requests"] != 0 || page.Identities[0].TotalRequests != 11 {
				t.Fatalf("metadata sync lost baseline: %+v", page)
			}
		})
	}
}

func TestUsageIdentityStatsResetValidatesIDAndAuthorization(t *testing.T) {
	db := openAPITestDatabase(t)
	seedUsageIdentityAliasAPIIdentity(t, db)
	for _, tc := range []struct {
		name, id     string
		intent, auth bool
		want         int
	}{
		{"invalid id", "no", true, false, http.StatusBadRequest},
		{"zero id", "0", true, false, http.StatusBadRequest},
		{"missing id", "999", true, false, http.StatusNotFound},
		{"missing intent", "1", false, false, http.StatusForbidden},
		{"unauthenticated", "1", true, true, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(nil, nil, nil, nil, AuthConfig{Enabled: tc.auth, LoginPassword: "test-password", SessionTTL: time.Hour}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/usage/identities/"+tc.id+"/stats/reset", nil)
			if tc.intent {
				request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			}
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
		})
	}
	row, err := repository.FindUsageIdentityByID(context.Background(), db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if row.StatsResetAt != nil {
		t.Fatal("rejected request changed baseline")
	}
}
