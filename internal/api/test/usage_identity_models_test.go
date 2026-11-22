package test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/service"
)

type identityModelsPayload struct {
	IdentityID string    `json:"identity_id"`
	Range      string    `json:"range"`
	Timezone   string    `json:"timezone"`
	RangeStart time.Time `json:"range_start"`
	RangeEnd   time.Time `json:"range_end"`
	Models     []struct {
		Model                string  `json:"model"`
		RequestCount         int64   `json:"request_count"`
		TotalTokens          int64   `json:"total_tokens"`
		CostUSD              float64 `json:"cost_usd"`
		UnavailableCostCount int64   `json:"unavailable_cost_count"`
		CostAvailable        bool    `json:"cost_available"`
	} `json:"models"`
	Summary struct {
		RequestCount         int64   `json:"request_count"`
		TotalTokens          int64   `json:"total_tokens"`
		CostUSD              float64 `json:"cost_usd"`
		UnavailableCostCount int64   `json:"unavailable_cost_count"`
		CostAvailable        bool    `json:"cost_available"`
	} `json:"summary"`
}

func TestUsageIdentityModelsNaturalDayRanges(t *testing.T) {
	for _, tc := range []struct {
		zone string
		now  time.Time
	}{
		{"UTC", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)},
		{"Asia/Shanghai", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)},
		{"America/Los_Angeles", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)},
		{"Europe/London", time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)},
		{"America/Santiago", time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			old := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = old })
			now := tc.now.In(location)
			todayNoon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, location)
			db := openAPITestDatabase(t)
			if err := db.Create(&[]entities.UsageIdentity{
				{ID: 1, Identity: "auth-1", AuthType: entities.UsageIdentityAuthTypeCredential, Name: "same"},
				{ID: 2, Identity: "auth-2", AuthType: entities.UsageIdentityAuthTypeAIProvider, Name: "same"},
			}).Error; err != nil {
				t.Fatal(err)
			}
			cost, available := 0.25, int64(0)
			var rows []entities.UsageOverviewDailyStat
			for day := -31; day <= 1; day++ {
				at := todayNoon.AddDate(0, 0, day)
				_, bucket := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: at})
				rows = append(rows, entities.UsageOverviewDailyStat{BucketStart: bucket.BucketStart, AuthIndex: "auth-1", APIGroupKey: "key", Model: "upstream", RequestCount: 1, TotalTokens: 10, CostUSD: &cost, UnavailableCostCount: &available})
			}
			if err := db.CreateInBatches(rows, 50).Error; err != nil {
				t.Fatal(err)
			}
			// 连明细表和 Key 表都不存在时，模型 HTTP 仍只能依靠已提交日桶。
			if err := db.Migrator().DropTable(&entities.UsageEvent{}, &entities.CPAAPIKey{}); err != nil {
				t.Fatal(err)
			}
			provider := service.NewUsageIdentityServiceWithOptions(db, nil, service.UsageIdentityServiceOptions{Now: func() time.Time { return now }})
			router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{UsageIdentity: provider})
			for _, test := range []struct {
				rangeValue string
				days       int
			}{{"today", 1}, {"7d", 7}, {"30d", 30}, {"", 7}} {
				response := serveAPIGet(router, "/api/v1/usage/identities/1/models?range="+test.rangeValue)
				if response.Code != http.StatusOK {
					t.Fatalf("%s: status %d: %s", test.rangeValue, response.Code, response.Body.String())
				}
				var body identityModelsPayload
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				wantRange := test.rangeValue
				if wantRange == "" {
					wantRange = "7d"
				}
				wantDate := todayNoon.AddDate(0, 0, -(test.days - 1)).Format(time.DateOnly)
				wantEnd := todayNoon.AddDate(0, 0, 1).Format(time.DateOnly)
				if body.IdentityID != "1" || body.Range != wantRange || body.Timezone != tc.zone || body.RangeStart.In(location).Format(time.DateOnly) != wantDate || body.RangeEnd.In(location).Format(time.DateOnly) != wantEnd {
					t.Fatalf("wrong natural-day boundaries: %+v, want %s to %s", body, wantDate, wantEnd)
				}
				if len(body.Models) != 1 || body.Models[0].Model != "upstream" || body.Models[0].RequestCount != int64(test.days) || body.Models[0].TotalTokens != int64(test.days*10) || body.Summary.RequestCount != int64(test.days) || !body.Summary.CostAvailable || math.Abs(body.Summary.CostUSD-float64(test.days)*0.25) > 1e-12 {
					t.Fatalf("wrong %s snapshot: %+v", test.rangeValue, body)
				}
			}
			response := serveAPIGet(router, "/api/v1/usage/identities/2/models?range=30d")
			var empty identityModelsPayload
			if err := json.Unmarshal(response.Body.Bytes(), &empty); err != nil || response.Code != http.StatusOK || len(empty.Models) != 0 || !strings.Contains(response.Body.String(), `"models":[]`) {
				t.Fatalf("empty identity: %d %s (%v)", response.Code, response.Body.String(), err)
			}
		})
	}
}

func TestUsageIdentityModelsPermissionsCostsAndReset(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 2, Identity: "provider-one", Name: "Provider", TotalRequests: 2}).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(now.In(time.Local).Year(), now.In(time.Local).Month(), now.In(time.Local).Day(), 0, 0, 0, 0, time.Local)
	price, zero, available, unavailable := 1.75, 0.0, int64(0), int64(1)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: day, AuthIndex: "provider-one", APIGroupKey: "key-a", Model: "upstream", RequestCount: 1, TotalTokens: 100, CostUSD: &price, UnavailableCostCount: &available},
		{BucketStart: day, AuthIndex: "provider-one", APIGroupKey: "key-deleted", Model: "upstream", RequestCount: 1, TotalTokens: 200, CostUSD: &zero, UnavailableCostCount: &unavailable},
		{BucketStart: day, AuthIndex: "provider-one", APIGroupKey: "key-a", Model: "unpriced", RequestCount: 1, TotalTokens: 10, CostUSD: &zero, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageIdentityServiceWithOptions(db, nil, service.UsageIdentityServiceOptions{Now: func() time.Time { return now }})
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatal(err)
	}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	gate := service.NewCostReadGate()
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "", keeperapi.OptionalProviders{UsageIdentity: provider, CostReadGate: gate, CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "viewer"}}})
	admin := &http.Cookie{Name: standardSessionCookieName, Value: adminToken}
	viewer := &http.Cookie{Name: standardSessionCookieName, Value: viewerToken}
	path := "/api/v1/usage/identities/1/models?range=today"
	if response := serveAPIGet(router, path); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d %s", response.Code, response.Body.String())
	}
	if response := serveAPIGet(router, path, viewer); response.Code != http.StatusForbidden {
		t.Fatalf("viewer: %d %s", response.Code, response.Body.String())
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/v1/usage/identities/no/models?range=today", http.StatusBadRequest},
		{"/api/v1/usage/identities/0/models?range=today", http.StatusBadRequest},
		{"/api/v1/usage/identities/999/models?range=today", http.StatusNotFound},
		{"/api/v1/usage/identities/1/models?range=24h", http.StatusBadRequest},
	} {
		if response := serveAPIGet(router, tc.path, admin); response.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
	read := func() identityModelsPayload {
		t.Helper()
		response := serveAPIGet(router, path, admin)
		if response.Code != http.StatusOK {
			t.Fatalf("models: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "key-deleted") || strings.Contains(response.Body.String(), "provider-one") {
			t.Fatalf("unexpected identity or API key in models response: %s", response.Body.String())
		}
		var body identityModelsPayload
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	before := read()
	if len(before.Models) != 2 || before.Summary.RequestCount != 3 || before.Summary.TotalTokens != 310 || math.Abs(before.Summary.CostUSD-1.75) > 1e-12 || before.Summary.CostAvailable || before.Summary.UnavailableCostCount != 2 {
		t.Fatalf("incomplete pricing snapshot: %+v", before)
	}
	for _, model := range before.Models {
		if model.CostAvailable || model.UnavailableCostCount != 1 {
			t.Fatalf("missing pricing availability: %+v", model)
		}
	}
	unpriced, upstream := before.Models[0], before.Models[1]
	if upstream.Model != "upstream" || upstream.RequestCount != 2 || upstream.TotalTokens != 300 || math.Abs(upstream.CostUSD-1.75) > 1e-12 {
		t.Fatalf("cross-key model aggregation: %+v", upstream)
	}
	if unpriced.Model != "unpriced" || math.Abs(unpriced.CostUSD) > 1e-12 {
		t.Fatalf("all-unpriced model: %+v", unpriced)
	}
	reset := httptest.NewRequest(http.MethodPost, "/api/v1/usage/identities/1/stats/reset", nil)
	reset.AddCookie(admin)
	reset.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, reset)
	if response.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", response.Code, response.Body.String())
	}
	after := read()
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("reset changed model snapshot: before=%+v after=%+v", before, after)
	}
	resume, err := gate.BlockAndDrain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	if response := serveAPIGet(router, path, admin); response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "costs_busy") {
		t.Fatalf("fee gate: %d %s", response.Code, response.Body.String())
	}
	if response := serveAPIGet(router, path, viewer); response.Code != http.StatusForbidden {
		t.Fatalf("gate preceded permission: %d %s", response.Code, response.Body.String())
	}
}

func TestUsageIdentityModelsDoesNotCatchUpPendingEvents(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 1, Identity: "auth"}).Error; err != nil {
		t.Fatal(err)
	}
	_, bucket := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: now})
	price, unavailable, available := 0.5, int64(0), true
	if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: bucket.BucketStart, AuthIndex: "auth", APIGroupKey: "key", Model: "model", RequestCount: 1, TotalTokens: 10, CostUSD: &price, UnavailableCostCount: &unavailable}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.UsageEvent{EventKey: "pending", Timestamp: now, AuthIndex: "auth", AuthType: "oauth", APIGroupKey: "key", Model: "model", TotalTokens: 900, CostUSD: &price, CostAvailable: &available}).Error; err != nil {
		t.Fatal(err)
	}
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
	response := serveAPIGet(router, "/api/v1/usage/identities/1/models?range=today")
	var body identityModelsPayload
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK || body.Summary.RequestCount != 1 || body.Summary.TotalTokens != 10 {
		t.Fatalf("pending event leaked into committed snapshot: %d %s (%v)", response.Code, response.Body.String(), err)
	}
}

func TestUsageIdentityModelsRejectsUnbackfilledCosts(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 1, Identity: "auth"}).Error; err != nil {
		t.Fatal(err)
	}
	_, bucket := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: now})
	priced, available := 1.0, int64(0)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: bucket.BucketStart, AuthIndex: "auth", APIGroupKey: "a", Model: "model", CostUSD: &priced, UnavailableCostCount: &available},
		{BucketStart: bucket.BucketStart, AuthIndex: "auth", APIGroupKey: "b", Model: "model", UnavailableCostCount: &available},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageIdentityService(db)
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{UsageIdentity: provider})
	response := serveAPIGet(router, "/api/v1/usage/identities/1/models?range=today")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"models"`) {
		t.Fatalf("NULL cost should fail rather than expose a partial total: %d %s", response.Code, response.Body.String())
	}
}
