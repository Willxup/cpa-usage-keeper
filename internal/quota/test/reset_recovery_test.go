package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestCodexResetRecoversCPARoutingAfterOfficialSuccess(t *testing.T) {
	for _, tc := range []struct {
		name            string
		consumeStatus   int
		consumeBody     string
		recoveryStatus  int
		recordResponses bool
		wantResetError  bool
	}{
		{name: "success", consumeStatus: 200, consumeBody: `{"code":"reset","windows_reset":2}`, recoveryStatus: 200},
		{name: "response recording enabled", consumeStatus: 200, consumeBody: `{"code":"reset","windows_reset":2}`, recoveryStatus: 200, recordResponses: true},
		{name: "recovery failure preserves consumed credit", consumeStatus: 200, consumeBody: `{"code":"reset","windows_reset":2}`, recoveryStatus: 500},
		{name: "older CPA without recovery endpoint", consumeStatus: 200, consumeBody: `{"code":"reset","windows_reset":2}`, recoveryStatus: 404},
		{name: "official rejection skips recovery", consumeStatus: 429, consumeBody: `{"error":"exhausted"}`, wantResetError: true},
		{name: "invalid official success skips recovery", consumeStatus: 200, consumeBody: `{"code":"unknown"}`, wantResetError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- r.URL.Path
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-management-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected method or management authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v0/management/api-call":
					var request apicall.Request
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.AuthIndex != "codex-auth" || request.URL != quota.CodexRateLimitResetCreditsConsumeURL {
						t.Errorf("unexpected consume request: %+v, error: %v", request, err)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"status_code": tc.consumeStatus, "body": tc.consumeBody})
				case "/v0/management/reset-quota":
					var request map[string]string
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !reflect.DeepEqual(request, map[string]string{"auth_index": "codex-auth"}) {
						t.Errorf("recovery must target only the current auth_index: %v, error: %v", request, err)
					}
					w.WriteHeader(tc.recoveryStatus)
					_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "auth_index": "codex-auth"})
				default:
					t.Errorf("unexpected CPA operation: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			db := openQuotaTestDatabase(t)
			disabled := true
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: &disabled})
			service := quota.NewServiceWithOptions(db, cpa.NewClient(server.URL, "test-management-key", time.Second, false), quota.ServiceOptions{
				PricingCatalog: emptyPricingCatalogForTest(), QuotaUpstreamResponsesEnabled: tc.recordResponses,
			})
			t.Cleanup(service.StopRefreshTasks)

			response, err := service.Reset(context.Background(), quota.ResetRequest{AuthIndex: " codex-auth "})
			if (err != nil) != tc.wantResetError {
				t.Fatalf("Reset error = %v, want error %v", err, tc.wantResetError)
			}
			wantCalls := []string{"/v0/management/api-call"}
			if !tc.wantResetError {
				wantCalls = append(wantCalls, "/v0/management/reset-quota")
				if response.AuthIndex != "codex-auth" || response.Code != "reset" || response.WindowsReset != 2 {
					t.Fatalf("official reset result lost: %+v", response)
				}
				encoded, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				var payload struct {
					RecoveryFailed bool `json:"recoveryFailed"`
				}
				if err := json.Unmarshal(encoded, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.RecoveryFailed != (tc.recoveryStatus != 200) {
					t.Errorf("partial success must expose recoveryFailed: %s", encoded)
				}
			}
			var gotCalls []string
			for len(calls) > 0 {
				gotCalls = append(gotCalls, <-calls)
			}
			if !reflect.DeepEqual(gotCalls, wantCalls) {
				t.Errorf("CPA calls = %v, want %v", gotCalls, wantCalls)
			}
		})
	}
}

type blockingQuotaRecoveryCaller struct {
	recordingManagementCaller
	entered chan struct{}
	release chan struct{}
}

func (c *blockingQuotaRecoveryCaller) ResetQuota(ctx context.Context, _ string) error {
	close(c.entered)
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCodexResetBlocksDuplicateConsumptionDuringCPARecovery(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	caller := &blockingQuotaRecoveryCaller{
		recordingManagementCaller: recordingManagementCaller{responses: []*apicall.Response{{StatusCode: 200, BodyText: `{"code":"reset","windows_reset":2}`}}},
		entered:                   make(chan struct{}), release: make(chan struct{}),
	}
	service := quota.NewService(db, caller, emptyPricingCatalogForTest())
	t.Cleanup(service.StopRefreshTasks)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.Reset(ctx, quota.ResetRequest{AuthIndex: "codex-auth"})
		done <- err
	}()
	select {
	case <-caller.entered:
	case <-ctx.Done():
		t.Fatal("reset did not reach CPA recovery")
	}
	_, err := service.Reset(ctx, quota.ResetRequest{AuthIndex: "codex-auth"})
	if !errors.Is(err, quota.ErrResetInProgress) {
		t.Errorf("duplicate reset during recovery should be rejected, got %v", err)
	}
	close(caller.release)
	if err := <-done; err != nil {
		t.Fatalf("original reset failed: %v", err)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("consumed reset credit %d times, want 1", len(caller.requests))
	}
}
