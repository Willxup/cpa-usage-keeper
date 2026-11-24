package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestMetaRefreshUsesSharedCacheWithoutHistoryOrWindowBackfill(t *testing.T) {
	for _, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled, quota.RefreshSourceInspection} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/response-viewer-%t", source, enabled), func(t *testing.T) {
				var downloads, calls atomic.Int32
				body := fmt.Sprintf(`{"api_key":"LLM|upstream-fixture","subs_tier_name":"Everyday Usage","subs_usage":{"window":{"used_percent":25,"window_duration_mins":300,"resets_at":%d},"weekly":{"used_percent":50,"resets_at":%d}}}`, time.Now().Add(time.Hour).Unix(), time.Now().Add(24*time.Hour).Unix())
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v8/management/credentials/download":
						downloads.Add(1)
						_, _ = fmt.Fprint(w, `{"dca_token":"dca:fixture-secret","api_key":"LLM|auth-file-secret"}`)
					case "/v8/management/requests/api-call":
						calls.Add(1)
						var request apicall.Request
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Header["Authorization"] != "Bearer dca:fixture-secret" {
							t.Error("missing expected DCA header")
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": body})
					default:
						t.Errorf("unexpected CPA path %s", r.URL.Path)
					}
				}))
				defer server.Close()
				db := openQuotaTestDatabase(t)
				if err := db.AutoMigrate(&entities.CodexQuotaCycle{}, &entities.CodexQuotaPercentSegment{}); err != nil {
					t.Fatal(err)
				}
				for _, auth := range []string{"meta-one", "meta-two"} {
					seedUsageIdentity(t, db, entities.UsageIdentity{Identity: auth, Provider: "meta", Type: "meta", AuthType: entities.UsageIdentityAuthTypeCredential, FileName: stringPtr(auth + ".json"), FilePath: stringPtr("/auth/" + auth + ".json")})
				}
				seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "meta-api-key", Provider: "meta", Type: "meta", AuthType: entities.UsageIdentityAuthTypeAIProvider})
				service := quota.NewServiceWithOptions(db, cpa.NewClient(server.URL, "management-key", time.Second, false), quota.ServiceOptions{QuotaUpstreamResponsesEnabled: enabled})
				t.Cleanup(service.StopRefreshTasks)
				setRefreshCooldown(service, func(time.Duration) {})
				switch source {
				case quota.RefreshSourceManual:
					result, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"meta-one", "meta-two", "meta-api-key"}, Source: source})
					if err != nil || result.Accepted != 2 || len(result.Rejected) != 1 || result.Rejected[0].Error != "not_credential" {
						t.Fatalf("batch credential separation: %+v, err=%v", result, err)
					}
				case quota.RefreshSourceScheduled:
					if err := service.RunAutoRefresh(context.Background()); err != nil {
						t.Fatal(err)
					}
				case quota.RefreshSourceInspection:
					if _, err := service.StartInspection(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				for _, auth := range []string{"meta-one", "meta-two"} {
					task := waitForRefreshTask(t, service, auth, quota.RefreshTaskStatusCompleted)
					if task.Quota == nil || task.Quota.Subscription == nil || task.Quota.Subscription.Plan != "Everyday Usage" || len(task.Quota.Quota) != 2 {
						t.Fatalf("missing Meta quota: %+v", task.Quota)
					}
					for _, row := range task.Quota.Quota {
						if row.WindowUsageTokens != nil || row.WindowUsageCost != nil {
							t.Fatalf("Meta window was backfilled: %+v", row)
						}
					}
					if enabled && (len(task.UpstreamResponses) != 1 || task.UpstreamResponses[0].Body != body) || !enabled && len(task.UpstreamResponses) != 0 {
						t.Fatalf("unexpected opt-in raw response count: %d", len(task.UpstreamResponses))
					}
					encoded, _ := json.Marshal(task)
					if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "auth-file-secret") || !enabled && strings.Contains(string(encoded), "upstream-fixture") {
						t.Fatal("credential or disabled response leaked into task")
					}
				}
				cache, err := service.GetCachedQuota(context.Background(), quota.CacheRequest{AuthIndexes: []string{"meta-one", "meta-two", "meta-api-key"}})
				if err != nil || len(cache.Items) != 2 || int(downloads.Load()) != 2 || int(calls.Load()) != 2 {
					t.Fatalf("cache/dispatch mismatch: items=%d downloads=%d calls=%d err=%v", len(cache.Items), downloads.Load(), calls.Load(), err)
				}
				for _, item := range cache.Items {
					if enabled && (len(item.UpstreamResponses) != 1 || item.UpstreamResponses[0].Body != body) || !enabled && len(item.UpstreamResponses) != 0 {
						t.Fatal("cached raw viewer behavior mismatch")
					}
				}
				service.StopRefreshTasks()
				var historyCount int64
				if err := db.Table("codex_quota_cycles").Count(&historyCount).Error; err != nil || historyCount != 0 {
					t.Fatalf("Meta must not create history: count=%d err=%v", historyCount, err)
				}
			})
		}
	}
}

func TestMetaFailedQuotaResponseCanBeViewedWithoutLeakingInError(t *testing.T) {
	body := `{"error":{"message":"LLM|upstream-secret"},"api_key":"LLM|upstream-secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/credentials/download" {
			_, _ = fmt.Fprint(w, `{"dca_token":"dca:auth-secret"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 401, "body": body})
	}))
	defer server.Close()
	db := openQuotaTestDatabase(t)
	input := metaInput().Identity
	seedUsageIdentity(t, db, input)
	service := quota.NewServiceWithOptions(db, cpa.NewClient(server.URL, "management-key", time.Second, false), quota.ServiceOptions{QuotaUpstreamResponsesEnabled: true})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	queueManualQuotaRefresh(t, service, input.Identity)
	task := waitForRefreshTask(t, service, input.Identity, quota.RefreshTaskStatusFailed)
	if task.HTTPStatusCode == nil || *task.HTTPStatusCode != 401 || strings.Contains(task.Error, "secret") || len(task.UpstreamResponses) != 1 || task.UpstreamResponses[0].Body != body {
		t.Fatalf("unexpected safe error / opt-in response: status=%v error=%q count=%d", task.HTTPStatusCode, task.Error, len(task.UpstreamResponses))
	}
	encoded, _ := json.Marshal(task.UpstreamResponses)
	if strings.Contains(string(encoded), "dca:auth-secret") {
		t.Fatal("DCA request header entered response collector")
	}
}
