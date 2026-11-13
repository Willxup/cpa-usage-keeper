package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestUsageIdentitySIWCNullableListPageAndDetail(t *testing.T) {
	db := openAPITestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 11, 2, 3, 30, 0, 0, time.FixedZone("west", -7*3600))
	rows := []entities.UsageIdentity{
		{Identity: "known", Type: "codex", SIWCBindSupported: new(false), SIWCBound: new(true), UseSIWC: new(false), SIWCStatus: new("future"), SIWCExpiresAt: &expiry},
		{Identity: "unknown", Type: "claude"},
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, rows, entities.UsageIdentityAuthTypeCredential, now); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
	read := func(path string) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", path, response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	assertKnown := func(row map[string]any) {
		t.Helper()
		if row["siwc_bind_supported"] != false || row["siwc_bound"] != true || row["use_siwc"] != false || row["siwc_status"] != "future" {
			t.Fatalf("known values = %+v", row)
		}
		value, ok := row["siwc_expires_at"].(string)
		if !ok {
			t.Fatalf("missing expiry: %+v", row)
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || !parsed.Equal(expiry) {
			t.Fatalf("expiry %q: %v", value, err)
		}
	}
	assertUnknown := func(row map[string]any) {
		t.Helper()
		for _, field := range []string{"siwc_bind_supported", "siwc_bound", "use_siwc", "siwc_status", "siwc_expires_at"} {
			if value, ok := row[field]; !ok || value != nil {
				t.Fatalf("%s = %v, present %v", field, value, ok)
			}
		}
	}
	for _, path := range []string{"/api/v1/usage/identities", "/api/v1/usage/identities/page?auth_type=1"} {
		listed := read(path)["identities"].([]any)
		if len(listed) != 2 {
			t.Fatalf("%s count = %d", path, len(listed))
		}
		for _, item := range listed {
			row := item.(map[string]any)
			switch row["identity"] {
			case "known":
				assertKnown(row)
				assertKnown(read("/api/v1/usage/identities/" + row["id"].(string)))
			case "unknown":
				assertUnknown(row)
				assertUnknown(read("/api/v1/usage/identities/" + row["id"].(string)))
			default:
				t.Fatalf("unexpected identity: %+v", row)
			}
		}
	}
}
