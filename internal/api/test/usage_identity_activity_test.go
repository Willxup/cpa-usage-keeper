package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestUsageIdentityActivityRouteUsesRealEventsAndAdminPermission(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC)
	if err := db.Create(&[]entities.UsageIdentity{
		{ID: 1, AuthType: 1, Identity: "shared"},
		{ID: 2, AuthType: 2, Identity: "shared"},
		{ID: 3, AuthType: 1, Identity: "quiet"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	code := 429
	events := []entities.UsageEvent{
		{EventKey: "oauth-failed", AuthType: "oauth", AuthIndex: "shared", Timestamp: now.Add(-time.Minute), Failed: true, StatusCode: &code},
		{EventKey: "oauth-success", AuthType: "oauth", AuthIndex: "shared", Timestamp: now.Add(-30 * time.Second)},
		{EventKey: "provider-failed", AuthType: "apikey", AuthIndex: "shared", Timestamp: now.Add(-time.Second), Failed: true},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
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
	provider := service.NewUsageIdentityServiceWithOptions(db, nil, service.UsageIdentityServiceOptions{Now: func() time.Time { return now }})
	router := keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, sessions), "", keeperapi.OptionalProviders{UsageIdentity: provider, CostReadGate: gate, CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "viewer"}}})
	admin := &http.Cookie{Name: standardSessionCookieName, Value: adminToken}
	viewer := &http.Cookie{Name: standardSessionCookieName, Value: viewerToken}
	path := "/api/v1/usage/identities/1/activity"
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
		{"/api/v1/usage/identities/no/activity", http.StatusBadRequest},
		{"/api/v1/usage/identities/0/activity", http.StatusBadRequest},
		{"/api/v1/usage/identities/999/activity", http.StatusNotFound},
	} {
		if response := serveAPIGet(router, tc.path, admin); response.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
	read := func(path string) map[string]any {
		t.Helper()
		response := serveAPIGet(router, path, admin)
		if response.Code != http.StatusOK {
			t.Fatalf("activity: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "shared") {
			t.Fatalf("raw auth_index leaked: %s", response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	body := read(path)
	if body["identity_id"] != "1" || body["streak"] != float64(1) || body["streak_at_least"] != false {
		t.Fatalf("wrong activity response: %+v", body)
	}
	latest := body["latest"].(map[string]any)
	failure := body["last_failure"].(map[string]any)
	if latest["failed"] != false || latest["status_code"] != nil || failure["failed"] != true || failure["status_code"] != float64(429) {
		t.Fatalf("wrong real event status: %+v", body)
	}
	providerBody := read("/api/v1/usage/identities/2/activity")
	if providerBody["last_success"] != nil || providerBody["latest"].(map[string]any)["failed"] != true {
		t.Fatalf("wrong type isolation: %+v", providerBody)
	}
	quiet := read("/api/v1/usage/identities/3/activity")
	if quiet["latest"] != nil || quiet["last_success"] != nil || quiet["last_failure"] != nil || quiet["streak"] != float64(0) {
		t.Fatalf("wrong empty state: %+v", quiet)
	}
	resume, err := gate.BlockAndDrain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	if response := serveAPIGet(router, path, admin); response.Code != http.StatusOK {
		t.Fatalf("non-cost activity blocked by cost gate: %d %s", response.Code, response.Body.String())
	}
	if response := serveAPIGet(router, "/api/v1/usage/identities/1/models?range=today", admin); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("models should remain cost-gated: %d %s", response.Code, response.Body.String())
	}
}

func TestUsageIdentityActivityRouteReportsBoundedStreak(t *testing.T) {
	db := openAPITestDatabase(t)
	now := time.Now()
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 2, Identity: "auth"}).Error; err != nil {
		t.Fatal(err)
	}
	var events []entities.UsageEvent
	for i := 0; i < 101; i++ {
		events = append(events, entities.UsageEvent{EventKey: fmt.Sprint(i), AuthType: "apikey", AuthIndex: "auth", Timestamp: now.Add(-time.Duration(i+1) * time.Second)})
	}
	if err := db.CreateInBatches(events, 50).Error; err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageIdentityServiceWithOptions(db, nil, service.UsageIdentityServiceOptions{Now: func() time.Time { return now }})
	router := keeperapi.NewRouter(nil, nil, nil, nil, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{UsageIdentity: provider})
	response := serveAPIGet(router, "/api/v1/usage/identities/1/activity")
	var body struct {
		Streak  int  `json:"streak"`
		AtLeast bool `json:"streak_at_least"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK || body.Streak != 100 || !body.AtLeast {
		t.Fatalf("bounded streak: %d %s (%v)", response.Code, response.Body.String(), err)
	}
}
