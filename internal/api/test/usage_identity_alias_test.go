package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func TestUsageIdentityAliasPatchUpdatesAndClearsAlias(t *testing.T) {
	db := openUsageIdentityAliasAPIDatabase(t)
	seedUsageIdentityAliasAPIIdentity(t, db)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/usage/identities/1", bytes.NewBufferString(`{"alias":"  Friendly Auth  "}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var updated struct {
		Alias       *string `json:"alias"`
		Name        string  `json:"name"`
		DisplayName string  `json:"displayName"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Alias == nil || *updated.Alias != "Friendly Auth" || updated.DisplayName != "Friendly Auth" || updated.Name != "Upstream Auth" {
		t.Fatalf("unexpected aliased response: %+v", updated)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/usage/identities/1", bytes.NewBufferString(`{"alias":""}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected clear status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var cleared struct {
		Alias       *string `json:"alias"`
		DisplayName string  `json:"displayName"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if cleared.Alias != nil || cleared.DisplayName != "Upstream Auth" {
		t.Fatalf("expected cleared alias and fallback display name, got %+v", cleared)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/usage/identities/1", bytes.NewBufferString(`{"alias":"Team 🚀"}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected emoji alias status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var emoji struct {
		Alias       *string `json:"alias"`
		DisplayName string  `json:"displayName"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &emoji); err != nil {
		t.Fatalf("decode emoji response: %v", err)
	}
	if emoji.Alias == nil || *emoji.Alias != "Team 🚀" || emoji.DisplayName != "Team 🚀" {
		t.Fatalf("expected emoji alias to be preserved, got %+v", emoji)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/usage/identities/1", bytes.NewBufferString(`{"alias":null}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected null alias clear status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestUsageIdentityAliasPatchRejectsInvalidInputAndDeletedRows(t *testing.T) {
	db := openUsageIdentityAliasAPIDatabase(t)
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
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(resp, req)
		if resp.Code != tc.want {
			t.Fatalf("%s: expected status %d, got %d body=%s", tc.name, tc.want, resp.Code, resp.Body.String())
		}
	}

	if err := repository.ReplaceUsageIdentitiesForAuthType(context.Background(), db, nil, entities.UsageIdentityAuthTypeAuthFile, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark identity deleted: %v", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/usage/identities/1", bytes.NewBufferString(`{"alias":"ok"}`))
	req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)
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

func openUsageIdentityAliasAPIDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-identity-alias-api.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
