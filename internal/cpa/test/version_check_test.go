package cpa_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestMinimumVersionUsesOnlyExplicitReleaseEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, version      string
		status             int
		verified, outdated bool
	}{
		{"old_success", "v7.9.9", 200, false, true},
		{"below_minimum", "8.0.11", 200, false, true},
		{"minimum", "v8.0.12", 200, true, false},
		{"newer", "8.1.0", 200, true, false},
		{"next_major", "v9.0.0", 200, true, false},
		{"old_failure", "v8.0.11", 503, false, true},
		{"unknown_success", "", 200, false, false},
		{"dev", "dev", 404, false, false},
		{"fork", "v7.9.9-custom", 200, false, false},
		{"malformed", "v7.1", 200, false, false},
		{"overflow", "18446744073709551616.0.0", 200, false, false},
		{"generic_404", "", 404, false, false},
		{"gateway_failure", "", 502, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer private-key" {
					t.Errorf("incorrect diagnostic request: method=%s auth=%q", r.Method, r.Header.Get("Authorization"))
				}
				if r.URL.Path != "/base/v0/management/debug" || r.URL.RawQuery != "" {
					t.Errorf("unexpected diagnostic path: %s", r.URL)
				}
				w.Header().Set("X-CPA-VERSION", tc.version)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("not JSON; this body must not be decoded or reported"))
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL+"/base", "private-key", time.Second, false)
			verified, err := client.CheckMinimumVersion(context.Background())
			if verified != tc.verified || (err != nil) != tc.outdated || requests != 1 {
				t.Fatalf("verified=%v error=%v requests=%d", verified, err, requests)
			}
			if err != nil && (!strings.Contains(err.Error(), "Please upgrade CPA to v8.0.12 or later") || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "not JSON")) {
				t.Fatalf("unsafe or unactionable diagnostic: %v", err)
			}
		})
	}
}

func TestMinimumVersionDoesNotFollowRedirectOrWaitForBody(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers_only", true: "redirect"}[redirect], func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if redirect {
					w.Header().Set("Location", "/unexpected")
					w.WriteHeader(http.StatusFound)
				} else {
					w.Header().Set("X-CPA-VERSION", "v8.0.12")
					w.WriteHeader(http.StatusOK)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL, "key", time.Second, false)
			start := time.Now()
			verified, err := client.CheckMinimumVersion(context.Background())
			if err != nil || verified != !redirect || requests != 1 || time.Since(start) > 500*time.Millisecond {
				t.Fatalf("headers-only diagnostic: verified=%v requests=%d elapsed=%s error=%v", verified, requests, time.Since(start), err)
			}
		})
	}
}

func TestMinimumVersionAllowsTemporaryFailureAndHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := cpa.NewClient(server.URL, "key", 20*time.Millisecond, false)
	if verified, err := client.CheckMinimumVersion(context.Background()); verified || err != nil {
		t.Fatalf("temporary timeout outdated offline upgrade: verified=%v error=%v", verified, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.CheckMinimumVersion(ctx); err != context.Canceled {
		t.Fatalf("caller cancellation ignored: %v", err)
	}
}
