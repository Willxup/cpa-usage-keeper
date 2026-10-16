package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/entities"
)

// 无论上游版本或可用性如何，旧库都应完成迁移，并通过业务 HTTP 读取本地历史。
func TestPricingLegacyStartupKeepsHistoryAvailableWithoutCPAVersionGate(t *testing.T) {
	for _, scenario := range []string{"old_release", "supported", "unknown", "authentication_failure", "unreachable", "slow"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := pricingRuntimeConfig(t)
			originalHooks := logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
			hook := logtest.NewGlobal()
			t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(originalHooks) })
			var probes atomic.Int32
			probeStarted := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v0/management/debug" {
					if probes.Add(1) == 1 {
						close(probeStarted)
					}
					if scenario == "slow" {
						<-r.Context().Done()
						return
					}
				}
				switch scenario {
				case "supported":
					w.Header().Set("X-CPA-VERSION", "v8.0.12")
				case "unknown":
					w.Header().Set("X-CPA-VERSION", "dev")
				case "old_release":
					w.Header().Set("X-CPA-VERSION", "v8.0.11")
				}
				if scenario == "authentication_failure" {
					w.WriteHeader(http.StatusUnauthorized)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(upstream.Close)
			cfg.CPABaseURL = upstream.URL
			if scenario == "unreachable" {
				upstream.Close()
			}
			seed := seedPublishedPricingRuntime(t, cfg)
			closePricingStartupPools(t, seed, seed)
			runtime := startPricingEvidenceRuntime(t, cfg)
			application := runtime.app
			baseURL := "http://" + cfg.ListenAddress() + cfg.AppBasePath
			waitPricingRuntimePhase(t, baseURL, "ready")
			status, body := pricingRuntimeRequest(t, http.MethodGet, baseURL+"/readyz", nil)
			if status != http.StatusOK {
				t.Fatalf("CPA %s blocked readiness: %d %s", scenario, status, body)
			}
			status, body = pricingRuntimeRequest(t, http.MethodGet, baseURL+"/api/v1/usage/events?range=custom&start=2026-09-23&end=2026-09-24", nil)
			if status != http.StatusOK || !strings.Contains(string(body), `"model":"old-model"`) || !strings.Contains(string(body), `"input_tokens":100`) {
				t.Fatalf("CPA %s prevented local history reads: %d %s", scenario, status, body)
			}
			var state entities.PricingMigrationState
			if err := application.DB.Take(&state).Error; err != nil || !state.DataComplete || state.BackupPath == nil {
				t.Fatalf("legacy migration did not complete safely: %+v %v", state, err)
			}
			if scenario != "unreachable" {
				select {
				case <-probeStarted:
				case <-time.After(2 * time.Second):
					t.Fatal("ready service did not run its background advisory")
				}
			}
			warnings := func() int {
				count := 0
				for _, entry := range hook.AllEntries() {
					if strings.Contains(entry.Message, "Please upgrade CPA") {
						if entry.Level != logrus.WarnLevel || entry.Message != "CPA 8.0.11 is below the minimum supported version. Please upgrade CPA to v8.0.12 or later. Keeper will continue running." {
							t.Fatalf("unexpected version warning: %+v", entry)
						}
						count++
					}
				}
				return count
			}
			if scenario == "old_release" {
				deadline := time.Now().Add(2 * time.Second)
				for warnings() == 0 && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
			}
			// 等待实际后台任务结束后再断言，避免把未完成请求误认作“不提示”。
			runtime.stop(t)
			wantWarnings := 0
			if scenario == "old_release" {
				wantWarnings = 1
			}
			if got := warnings(); got != wantWarnings {
				t.Fatalf("version warnings=%d, want %d", got, wantWarnings)
			}
			if scenario != "unreachable" && probes.Load() != 1 {
				t.Fatalf("expected one background check, got %d", probes.Load())
			}
		})
	}
}

func TestPricingUpgradeCompatibilityDoesNotProbeFreshOrCompletedDatabase(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.Header().Set("X-CPA-VERSION", "v7.9.9") }))
	defer server.Close()
	cfg := testAppConfig(t)
	cfg.CPABaseURL = server.URL
	for range 2 {
		application, err := keeperapp.NewWithConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := application.Initialize(context.Background()); err != nil {
			t.Fatalf("fresh/completed startup gained compatibility network dependency: %v", err)
		}
		if err := application.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 0 {
		t.Fatalf("fresh/completed startup made %d CPA probes", requests)
	}
}
