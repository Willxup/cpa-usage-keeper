package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/quota"
)

func TestMetaQuotaMissingAndInvalidValuesStayUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, body, plan, label string
		rows                    int
		percent                 *float64
		hasDuration, hasReset   bool
	}{
		{"empty object", `{}`, "", "", 0, nil, false, false},
		{"opaque tier", `{"subs_usage":{"tier":"1234567890123456","window":{"used_percent":null}}}`, "1234567890123456", "Current", 1, nil, false, false},
		{"unknown duration", `{"subs_usage":{"window":{"used_percent":0,"resets_at":1792272519}}}`, "", "Current", 1, floatPtr(0), false, true},
		{"other duration", `{"subs_usage":{"window":{"used_percent":"99.5","window_duration_mins":120,"resets_at":"1792272519"}}}`, "", "2h", 1, floatPtr(99.5), true, true},
		{"minute duration", `{"subs_usage":{"window":{"used_percent":25,"window_duration_mins":90}}}`, "", "90min", 1, floatPtr(25), true, false},
		{"invalid values", `{"subs_usage":{"window":{"used_percent":"NaN","window_duration_mins":"Infinity","resets_at":-5}}}`, "", "Current", 1, nil, false, false},
		{"clamped negative", `{"subs_usage":{"window":{"used_percent":-4}}}`, "", "Current", 1, floatPtr(0), false, false},
		{"clamped over 100", `{"subs_usage":{"weekly":{"used_percent":120}}}`, "", "Weekly", 1, floatPtr(100), false, false},
		{"missing window omitted", `{"subs_usage":{"weekly":{"used_percent":50}}}`, "", "Weekly", 1, floatPtr(50), false, false},
		{"non-string tier ignored", `{"subs_tier_name":123,"subs_usage":{"tier":"Future Usage"}}`, "Future Usage", "", 0, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, closeServer := metaFixtureProvider(t, `{"dca_token":"dca:fixture"}`, 200, tc.body)
			defer closeServer()
			output, err := provider.Check(context.Background(), metaInput())
			if err != nil {
				t.Fatal(err)
			}
			subscription := quota.NormalizeSubscription(output)
			if tc.plan == "" && subscription != nil || tc.plan != "" && (subscription == nil || subscription.Plan != tc.plan) {
				t.Fatalf("unexpected plan: %+v", subscription)
			}
			rows := quota.NormalizeQuotaRows(output)
			if len(rows) != tc.rows {
				t.Fatalf("rows = %+v", rows)
			}
			if len(rows) == 0 {
				return
			}
			row := rows[0]
			if row.Label != tc.label || (row.Window != nil) != tc.hasDuration || (row.ResetAt != "") != tc.hasReset || (row.UsedPercent == nil) != (tc.percent == nil) {
				t.Fatalf("row = %+v", row)
			}
			if tc.percent != nil {
				assertApproxFloatField(t, row.UsedPercent, *tc.percent, "percent")
			}
		})
	}
}

func TestMetaCredentialFailuresNeverUseAPIKeyOrLeakSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		status     int
	}{
		{"missing dca", `{"api_key":"LLM|secret"}`, 200},
		{"wrong dca prefix", `{"dca_token":"LLM|secret"}`, 200},
		{"blank dca", `{"dca_token":"dca:"}`, 200},
		{"whitespace inside dca", `{"dca_token":"dca:fixture secret"}`, 200},
		{"invalid json", `{"dca_token":"dca:secret"`, 200},
		{"array", `["dca:secret"]`, 200},
		{"download failure", `{"dca_token":"dca:secret"}`, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v8/management/credentials/download" {
					calls++
					t.Errorf("unexpected quota request: %s", r.URL.Path)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.file)
			}))
			defer server.Close()
			registry := quota.NewDefaultProviderRegistry(cpa.NewClient(server.URL, "management-key", time.Second, false), quota.DefaultProviderConfigs())
			provider, _ := registry.Provider("meta")
			_, err := provider.Check(context.Background(), metaInput())
			if err == nil || strings.Contains(err.Error(), "secret") || calls != 0 {
				t.Fatalf("credential failure: err=%v, quota calls=%d", err, calls)
			}
		})
	}
}

func TestMetaQuotaRejectsNonDownloadableAndInvalidResponses(t *testing.T) {
	provider, closeServer := metaFixtureProvider(t, `{"dca_token":"dca:fixture"}`, 200, `not-json with private data`)
	defer closeServer()
	input := metaInput()
	input.Identity.FilePath = nil
	if _, err := provider.Check(context.Background(), input); err == nil {
		t.Fatal("runtime-only credential unexpectedly accepted")
	}
	if _, err := provider.Check(context.Background(), metaInput()); err == nil || strings.Contains(err.Error(), "private data") {
		t.Fatalf("expected safe protocol error, got %v", err)
	}
}

func metaFixtureProvider(t *testing.T, file string, status int, body string) (quota.ProviderHandler, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials/download":
			_, _ = fmt.Fprint(w, file)
		case "/v8/management/requests/api-call":
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": status, "body": body})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	registry := quota.NewDefaultProviderRegistry(cpa.NewClient(server.URL, "management-key", time.Second, false), quota.DefaultProviderConfigs())
	provider, ok := registry.Provider("meta")
	if !ok {
		server.Close()
		t.Fatal("Meta provider not registered")
	}
	return provider, server.Close
}
