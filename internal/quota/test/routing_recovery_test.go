package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestQuotaRefreshRecoversCPACredentialCooldown(t *testing.T) {
	resetAt := time.Now().Add(72 * time.Hour).Unix()
	codexUsage := func(usedPercent int, allowed bool, extra string) string {
		return fmt.Sprintf(`{"rate_limit":{"allowed":%t,"limit_reached":%t,"primary_window":{"used_percent":%d,"limit_window_seconds":604800,"reset_at":%d}}%s,"rate_limit_reset_credits":{"available_count":0}}`,
			allowed, !allowed, usedPercent, resetAt, extra)
	}
	credentialCooldown := `[{"scope":"credential","reason":"credential_quota","retry_at":"2030-01-01T00:00:00Z","remaining_seconds":259200}]`
	for _, tc := range []struct {
		name       string
		provider   string
		usage      string
		cooldowns  string
		lookupCode int
		wantCalls  []string
	}{
		{
			name: "codex quota restored outside Keeper", provider: "codex",
			usage: codexUsage(0, true, ""), cooldowns: credentialCooldown,
			wantCalls: []string{"credentials", "reset"},
		},
		{
			name: "codex exhausted additional limit does not block recovery", provider: "codex",
			usage:     codexUsage(10, true, `,"additional_rate_limits":[{"limit_name":"spark","rate_limit":{"allowed":false,"limit_reached":true,"primary_window":{"used_percent":100,"limit_window_seconds":604800}}}]`),
			cooldowns: credentialCooldown, wantCalls: []string{"credentials", "reset"},
		},
		{
			name: "claude weekly restored with exhausted model sub-limit", provider: "claude",
			usage:     `{"five_hour":null,"seven_day":{"utilization":0,"resets_at":"2030-01-01T00:00:00Z"},"seven_day_opus":{"utilization":100,"resets_at":"2030-01-01T00:00:00Z"}}`,
			cooldowns: credentialCooldown, wantCalls: []string{"credentials", "reset"},
		},
		{
			name: "codex still exhausted skips CPA", provider: "codex",
			usage: codexUsage(100, false, ""), cooldowns: credentialCooldown,
		},
		{
			name: "claude still exhausted skips CPA", provider: "claude",
			usage:     `{"five_hour":{"utilization":20,"resets_at":"2030-01-01T00:00:00Z"},"seven_day":{"utilization":100,"resets_at":"2030-01-01T00:00:00Z"}}`,
			cooldowns: credentialCooldown,
		},
		{
			name: "claude missing utilization is not treated as 0%", provider: "claude",
			usage:     `{"five_hour":null,"seven_day":{"resets_at":"2030-01-01T00:00:00Z"}}`,
			cooldowns: credentialCooldown,
		},
		{
			name: "codex missing used_percent is not treated as 0%", provider: "codex",
			usage:     `{"rate_limit":{"allowed":true,"primary_window":{"limit_window_seconds":604800}},"rate_limit_reset_credits":{"available_count":0}}`,
			cooldowns: credentialCooldown,
		},
		{
			name: "codex invalid used_percent is not treated as available", provider: "codex",
			usage:     `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":"NaN","limit_window_seconds":18000},"secondary_window":{"used_percent":-1,"limit_window_seconds":604800}},"rate_limit_reset_credits":{"available_count":0}}`,
			cooldowns: credentialCooldown,
		},
		{
			name: "model-scoped cooldown is left to CPA", provider: "codex",
			usage:     codexUsage(0, true, ""),
			cooldowns: `[{"scope":"model","model_key":"gpt-a","reason":"quota","retry_at":"2030-01-01T00:00:00Z","remaining_seconds":259200}]`,
			wantCalls: []string{"credentials"},
		},
		{
			name: "short backoff cooldown is left to expire", provider: "codex",
			usage:     codexUsage(0, true, ""),
			cooldowns: `[{"scope":"credential","reason":"quota","retry_at":"2030-01-01T00:00:00Z","remaining_seconds":600}]`,
			wantCalls: []string{"credentials"},
		},
		{
			name: "unknown cooldown state is left alone", provider: "codex",
			usage: codexUsage(0, true, ""), cooldowns: `null`,
			wantCalls: []string{"credentials"},
		},
		{
			name: "cooldown lookup failure keeps refresh result", provider: "codex",
			usage: codexUsage(0, true, ""), cooldowns: credentialCooldown, lookupCode: http.StatusInternalServerError,
			wantCalls: []string{"credentials"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			record := func(call string) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, call)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v8/management/requests/api-call":
					var request apicall.Request
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("decode api-call: %v", err)
					}
					body := tc.usage
					if strings.Contains(request.URL, "/oauth/profile") {
						body = `{}`
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": body})
				case "/v8/management/credentials":
					record("credentials")
					if r.Method != http.MethodGet || r.URL.Query().Get("auth_index") != "auth-a" {
						t.Errorf("cooldown lookup must target only auth-a: %s %s", r.Method, r.URL.RawQuery)
					}
					if tc.lookupCode != 0 {
						w.WriteHeader(tc.lookupCode)
						return
					}
					_, _ = fmt.Fprintf(w, `{"files":[{"auth_index":"auth-a","provider":%q,"cooldowns":%s}]}`, tc.provider, tc.cooldowns)
				case "/v8/management/routing/cooldown/reset":
					record("reset")
					var request map[string]string
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !reflect.DeepEqual(request, map[string]string{"auth_index": "auth-a"}) {
						t.Errorf("reset must target only auth-a: %v, error: %v", request, err)
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "auth_index": "auth-a"})
				default:
					t.Errorf("unexpected CPA operation: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-a", Provider: tc.provider, Type: tc.provider, AuthType: entities.UsageIdentityAuthTypeCredential})
			service := quota.NewService(db, cpa.NewClient(server.URL, "test-management-key", time.Second, false))
			setRefreshCooldown(service, func(time.Duration) {})
			t.Cleanup(service.StopRefreshTasks)

			queueManualQuotaRefresh(t, service, "auth-a")
			waitForRefreshTask(t, service, "auth-a", quota.RefreshTaskStatusCompleted)
			// 恢复在结果发布后执行，等待 worker 退出而不取消其 context。
			service.WaitRefreshTasks()

			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("CPA routing calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}

// stalledSubscriptionCaller 让 Codex 额度立即成功、订阅查询耗尽任务 deadline，并记录恢复调用时的 context 状态。
type stalledSubscriptionCaller struct {
	codexRouteCaller
	resetContextErr chan error
}

func (c *stalledSubscriptionCaller) FetchCredentialCooldowns(ctx context.Context, _ string) ([]credentials.CredentialCooldown, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return []credentials.CredentialCooldown{{Scope: "credential", Reason: "credential_quota", RemainingSeconds: 259200}}, true, nil
}

func (c *stalledSubscriptionCaller) ResetQuota(ctx context.Context, _ string) error {
	c.resetContextErr <- ctx.Err()
	return nil
}

func TestQuotaRefreshRecoversAfterSubscriptionExhaustsTaskDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := openQuotaTestDatabase(t)
		accountID := "acct_123"
		seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeCredential, AccountID: &accountID})
		usageBody := fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":0,"limit_window_seconds":604800,"reset_at":%d}},"rate_limit_reset_credits":{"available_count":0}}`, time.Now().Add(72*time.Hour).Unix())
		caller := &stalledSubscriptionCaller{resetContextErr: make(chan error, 1)}
		caller.call = func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
			if strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return quotaAPIResponse(200, usageBody), nil
		}
		service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{})
		defer service.StopRefreshTasks()
		setRefreshCooldown(service, func(time.Duration) {})

		queueManualQuotaRefresh(t, service, "codex-auth")
		select {
		case err := <-caller.resetContextErr:
			if err != nil {
				t.Fatalf("recovery ran with a finished context: %v", err)
			}
		case <-time.After(2 * quota.RefreshTaskTimeout):
			t.Fatal("cooldown was not reset after the subscription query exhausted the task deadline")
		}
	})
}
