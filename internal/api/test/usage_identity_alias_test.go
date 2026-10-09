package test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func TestUsageIdentityAliasPatchUpdatesAndClearsAlias(t *testing.T) {
	db := openAPITestDatabase(t)
	seedUsageIdentityAliasAPIIdentity(t, db)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})

	for _, tc := range []struct {
		body    string
		alias   *string
		display string
	}{
		{`{"alias":"  Friendly Auth  "}`, new("Friendly Auth"), "Friendly Auth"},
		{`{"alias":""}`, nil, "Upstream Auth"},
		{`{"alias":"Team 🚀"}`, new("Team 🚀"), "Team 🚀"},
		{`{"alias":null}`, nil, "Upstream Auth"},
	} {
		resp := serveCredentialMutation(router, http.MethodPatch, "/api/v1/usage/identities/1", tc.body)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", tc.body, resp.Code, resp.Body.String())
		}
		var updated struct {
			Alias       *string `json:"alias"`
			Name        string  `json:"name"`
			DisplayName string  `json:"displayName"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &updated); err != nil {
			t.Fatalf("decode alias response: %v", err)
		}
		if !reflect.DeepEqual(updated.Alias, tc.alias) || updated.DisplayName != tc.display || updated.Name != "Upstream Auth" {
			t.Fatalf("%s returned unexpected alias/display: %+v", tc.body, updated)
		}
	}
}

func TestUsageIdentityAliasPatchRejectsInvalidInputAndDeletedRows(t *testing.T) {
	db := openAPITestDatabase(t)
	seedUsageIdentityAliasAPIIdentity(t, db)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})

	for _, tc := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "invalid id", path: "/api/v1/usage/identities/not-an-int", body: `{"alias":"ok"}`, want: http.StatusBadRequest},
		{name: "missing alias", path: "/api/v1/usage/identities/1", body: `{}`, want: http.StatusBadRequest},
		{name: "non string alias", path: "/api/v1/usage/identities/1", body: `{"alias":42}`, want: http.StatusBadRequest},
		{name: "too long", path: "/api/v1/usage/identities/1", body: `{"alias":"` + strings.Repeat("a", 51) + `"}`, want: http.StatusBadRequest},
		{name: "control char", path: "/api/v1/usage/identities/1", body: "{\"alias\":\"bad\\u0001alias\"}", want: http.StatusBadRequest},
		{name: "bidi override", path: "/api/v1/usage/identities/1", body: "{\"alias\":\"safe\\u202Eevil\"}", want: http.StatusBadRequest},
		{name: "zero width space", path: "/api/v1/usage/identities/1", body: "{\"alias\":\"safe\\u200Bname\"}", want: http.StatusBadRequest},
	} {
		resp := serveCredentialMutation(router, http.MethodPatch, tc.path, tc.body)
		if resp.Code != tc.want {
			t.Fatalf("%s: expected status %d, got %d body=%s", tc.name, tc.want, resp.Code, resp.Body.String())
		}
	}

	if err := repository.ReplaceUsageIdentitiesForAuthType(context.Background(), db, nil, entities.UsageIdentityAuthTypeAuthFile, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark identity deleted: %v", err)
	}
	resp := serveCredentialMutation(router, http.MethodPatch, "/api/v1/usage/identities/1", `{"alias":"ok"}`)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("deleted id: expected status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func seedUsageIdentityAliasAPIIdentity(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := repository.ReplaceUsageIdentitiesForAuthType(context.Background(), db, []entities.UsageIdentity{{
		Name:     "Upstream Auth",
		Identity: "auth-1",
		Type:     "codex",
		Provider: "Codex",
	}}, entities.UsageIdentityAuthTypeAuthFile, time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
}
