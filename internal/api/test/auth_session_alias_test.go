package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
)

func TestManagedSessionAliasPatchUpdatesAndClearsAdminAlias(t *testing.T) {
	router, manager, adminToken := newSessionAliasRouter(t)
	targetToken, _, err := manager.Create()
	if err != nil {
		t.Fatalf("create target admin session: %v", err)
	}
	targetID := auth.SessionTokenHash(targetToken)

	response := patchManagedSessionAlias(router, adminToken, targetID, `{"alias":"  Office Mac  "}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected alias update status 200, got %d body=%s", response.Code, response.Body.String())
	}
	var updated struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated session: %v", err)
	}
	if updated.ID != targetID || updated.Kind != "admin" || updated.Alias != "Office Mac" {
		t.Fatalf("unexpected updated session response: %+v", updated)
	}

	response = patchManagedSessionAlias(router, adminToken, targetID, `{"alias":null}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected alias clear status 200, got %d body=%s", response.Code, response.Body.String())
	}
	var cleared struct {
		Alias *string `json:"alias"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode cleared session: %v", err)
	}
	if cleared.Alias == nil || *cleared.Alias != "" {
		t.Fatalf("expected cleared alias to be returned as an empty string, got %s", response.Body.String())
	}
}

func TestManagedSessionAliasPatchRejectsAPIKeyUnknownAndInvalidTargets(t *testing.T) {
	router, manager, adminToken := newSessionAliasRouter(t)
	viewerToken, _, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("create API key session: %v", err)
	}

	for _, testCase := range []struct {
		name string
		id   string
		body string
		want int
	}{
		{name: "API key session", id: auth.SessionTokenHash(viewerToken), body: `{"alias":"Viewer"}`, want: http.StatusNotFound},
		{name: "unknown session", id: "missing", body: `{"alias":"Unknown"}`, want: http.StatusNotFound},
		{name: "missing alias", id: auth.SessionTokenHash(adminToken), body: `{}`, want: http.StatusBadRequest},
		{name: "non-string alias", id: auth.SessionTokenHash(adminToken), body: `{"alias":42}`, want: http.StatusBadRequest},
		{name: "too long", id: auth.SessionTokenHash(adminToken), body: `{"alias":"` + strings.Repeat("a", 51) + `"}`, want: http.StatusBadRequest},
		{name: "control character", id: auth.SessionTokenHash(adminToken), body: "{\"alias\":\"bad\\u0001alias\"}", want: http.StatusBadRequest},
		{name: "bidi override", id: auth.SessionTokenHash(adminToken), body: "{\"alias\":\"safe\\u202Eevil\"}", want: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := patchManagedSessionAlias(router, adminToken, testCase.id, testCase.body)
			if response.Code != testCase.want {
				t.Fatalf("expected status %d, got %d body=%s", testCase.want, response.Code, response.Body.String())
			}
		})
	}
}

func newSessionAliasRouter(t *testing.T) (http.Handler, *auth.SessionManager, string) {
	t.Helper()
	manager := auth.NewSessionManager(time.Hour)
	adminToken, _, err := manager.Create()
	if err != nil {
		t.Fatalf("create current admin session: %v", err)
	}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	handler := keeperapi.NewAuthHandler(config, manager)
	return keeperapi.NewRouter(nil, nil, nil, nil, config, handler, ""), manager, adminToken
}

func patchManagedSessionAlias(router http.Handler, adminToken, id, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/sessions/"+id, bytes.NewBufferString(body))
	request.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: adminToken})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
	router.ServeHTTP(response, request)
	return response
}
