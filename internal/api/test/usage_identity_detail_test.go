package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestUsageIdentityDetailMatchesListItemIncludingCachedHealth(t *testing.T) {
	for _, authType := range []entities.UsageIdentityAuthType{1, 2} {
		t.Run(fmt.Sprint(authType), func(t *testing.T) {
			db := openAPITestDatabase(t)
			now := time.Now()
			row := entities.UsageIdentity{ID: 1, AuthType: authType, Identity: "health-fixture", Name: "Fixture", Type: "codex", TotalRequests: 100, ResetTotalRequests: 99, StatsResetAt: &now}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			eventType := "oauth"
			if authType == 2 {
				eventType = "apikey"
			}
			for i, failed := range []bool{false, true} {
				event := entities.UsageEvent{EventKey: fmt.Sprint(i), AuthType: eventType, AuthIndex: row.Identity, Timestamp: now.Add(-time.Minute), Failed: failed, InputTokens: 100, CacheReadTokens: 25}
				if err := db.Create(&event).Error; err != nil {
					t.Fatal(err)
				}
			}
			cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cache.Close)
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityServiceWithRecentCache(db, cache)})
			read := func(path string) map[string]any {
				t.Helper()
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != http.StatusOK {
					t.Fatalf("status %d: %s", response.Code, response.Body.String())
				}
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				return body
			}
			page := read("/api/v1/usage/identities/page")
			listed := page["identities"].([]any)[0].(map[string]any)
			detail := read("/api/v1/usage/identities/1")
			health, ok := detail["credential_health"].(map[string]any)
			if !ok {
				t.Fatal("detail omitted cached health")
			}
			if health["total_success"] != float64(1) || health["total_failure"] != float64(1) || health["input_tokens"] != float64(200) || health["cache_read_tokens"] != float64(50) || len(health["buckets"].([]any)) != 30 {
				t.Fatalf("health must use the rolling cache independently of lifetime and reset counters: %+v", health)
			}
			// 若两次读取跨过 10 分钟边界，重新读取列表以比较同一窗口的完整响应。
			if listed["credential_health"].(map[string]any)["window_end"] != health["window_end"] {
				listed = read("/api/v1/usage/identities/page")["identities"].([]any)[0].(map[string]any)
			}
			if !reflect.DeepEqual(detail, listed) {
				t.Fatalf("detail differs from list item:\ndetail=%+v\nlist=%+v", detail, listed)
			}
		})
	}
}

func TestUsageIdentityDetailReadsOffPageAndDeletedIdentity(t *testing.T) {
	for _, authType := range []entities.UsageIdentityAuthType{1, 2} {
		t.Run(fmt.Sprint(authType), func(t *testing.T) {
			db := openAPITestDatabase(t)
			resetAt := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
			row := entities.UsageIdentity{ID: 1, AuthType: authType, Identity: "detail-fixture", Name: "Fixture", TotalRequests: 10, ResetTotalRequests: 8, StatsResetAt: &resetAt, IsDeleted: true}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			for id := int64(2); id <= 12; id++ {
				if err := db.Create(&entities.UsageIdentity{ID: id, AuthType: authType, Identity: fmt.Sprint(id), TotalRequests: 100}).Error; err != nil {
					t.Fatal(err)
				}
			}
			router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
			page := httptest.NewRecorder()
			router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/page?sort=total_requests&page_size=10", nil))
			var listed struct {
				Identities []struct{ ID string } `json:"identities"`
			}
			if err := json.Unmarshal(page.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if len(listed.Identities) != 10 {
				t.Fatalf("page: %s", page.Body.String())
			}
			for _, item := range listed.Identities {
				if item.ID == "1" {
					t.Fatal("fixture should be outside the current page")
				}
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/1", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("detail status %d: %s", response.Code, response.Body.String())
			}
			var detail struct {
				ID            string           `json:"id"`
				TotalRequests int64            `json:"total_requests"`
				Period        map[string]int64 `json:"period_stats"`
				StatsResetAt  time.Time        `json:"stats_reset_at"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if detail.ID != "1" || detail.TotalRequests != 10 || detail.Period["total_requests"] != 2 || !detail.StatsResetAt.Equal(resetAt) {
				t.Fatalf("detail: %s", response.Body.String())
			}
		})
	}
}

func TestUsageIdentityDetailValidatesIDAndAuthorization(t *testing.T) {
	db := openAPITestDatabase(t)
	seedUsageIdentityAliasAPIIdentity(t, db)
	for _, tc := range []struct {
		id   string
		auth bool
		want int
	}{
		{"no", false, http.StatusBadRequest},
		{"0", false, http.StatusBadRequest},
		{"999", false, http.StatusNotFound},
		{"1", true, http.StatusUnauthorized},
	} {
		t.Run(tc.id, func(t *testing.T) {
			router := NewRouter(nil, nil, nil, nil, AuthConfig{Enabled: tc.auth, LoginPassword: "test-password", SessionTTL: time.Hour}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/identities/"+tc.id, nil))
			if response.Code != tc.want {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
