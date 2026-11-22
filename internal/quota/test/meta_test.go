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
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestMetaQuotaUsesDownloadedDCAAndNormalizesWindows(t *testing.T) {
	var requests []apicall.Request
	var downloads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer management-key" {
			t.Error("missing CPA authentication")
		}
		switch r.URL.Path {
		case "/v8/management/credentials/download":
			downloads = append(downloads, r.URL.Query().Get("name"))
			_, _ = fmt.Fprint(w, `{"dca_token":" dca:fixture-dca ","api_key":"LLM|must-not-be-used"}`)
		case "/v8/management/requests/api-call":
			var request apicall.Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			requests = append(requests, request)
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": `{"api_key":"LLM|private","subs_tier_name":"High Usage","is_subs_active":true,"subs_usage":{"tier":"opaque-id","window":{"used_percent":"12.5","window_duration_mins":300,"resets_at":1792272519},"weekly":{"used_percent":0,"resets_at":1792368000}}}`})
		default:
			t.Errorf("unexpected CPA path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	registry := quota.NewDefaultProviderRegistry(cpa.NewClient(server.URL, "management-key", time.Second, false), quota.DefaultProviderConfigs())
	provider, ok := registry.Provider("meta")
	if !ok {
		t.Fatal("Meta auth-file quota provider is not registered")
	}
	output, err := provider.Check(context.Background(), metaInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 || downloads[0] != "meta + account.json" || len(requests) != 1 {
		t.Fatalf("unexpected downloads or request count: %v, %d", downloads, len(requests))
	}
	req := requests[0]
	if req.AuthIndex != "meta-auth" || req.Method != "POST" || req.URL != "https://api.meta.ai/muse-code/key" || req.Header["Authorization"] != "Bearer dca:fixture-dca" || req.Header["x-api-version"] != "1.0.0" || req.Header["Content-Type"] != "application/json" || req.Header["Accept"] != "application/json" || req.Data != "{}" {
		t.Fatal("unexpected Meta quota request contract")
	}
	if got := quota.NormalizeSubscription(output); got == nil || got.Provider != "meta" || got.Plan != "High Usage" {
		t.Fatalf("unexpected subscription: %+v", got)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 2 || rows[0].Label != "5h" || rows[1].Label != "Weekly" || rows[0].Window == nil || rows[0].Window.Seconds == nil || *rows[0].Window.Seconds != 18000 || rows[1].Window != nil {
		t.Fatalf("unexpected windows: %+v", rows)
	}
	assertApproxFloatField(t, rows[0].UsedPercent, 12.5, "current")
	assertApproxFloatField(t, rows[1].UsedPercent, 0, "weekly explicit zero")
	for index, expected := range []int64{1792272519, 1792368000} {
		reset, err := time.Parse(time.RFC3339, rows[index].ResetAt)
		if err != nil || reset.Unix() != expected {
			t.Fatalf("reset time for window %d = %q, want Unix %d", index, rows[index].ResetAt, expected)
		}
	}
	serialized, _ := json.Marshal(rows)
	if strings.Contains(string(serialized), "private") || strings.Contains(string(serialized), "fixture-dca") {
		t.Fatal("normal quota rows exposed upstream secrets")
	}
}

func metaInput() quota.ProviderInput {
	return quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "meta-auth", Provider: "meta", Type: "meta", AuthType: entities.UsageIdentityAuthTypeCredential, FileName: stringPtr("meta + account.json"), FilePath: stringPtr("/auth/meta + account.json")}}
}
