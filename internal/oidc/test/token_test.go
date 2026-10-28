package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"cpa-usage-keeper/internal/oidc"
)

func TestOIDCInvalidGrantCountsBeforeAndAfterSuccessfulLogin(t *testing.T) {
	for _, scenario := range []struct {
		name, style string
		methods     []string
	}{
		{"basic only", "basic", []string{"client_secret_basic"}},
		{"post only", "post", []string{"client_secret_post"}},
		{"default basic", "basic", nil},
		{"both basic", "basic", []string{"client_secret_basic", "client_secret_post"}},
		{"both post", "post", []string{"client_secret_basic", "client_secret_post"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			hook := captureOIDCLogs(t)
			m := newMockIDP(t, false, "")
			m.style = scenario.style
			m.metadata = func(metadata map[string]any) {
				delete(metadata, "token_endpoint_auth_methods_supported")
				if scenario.methods != nil {
					metadata["token_endpoint_auth_methods_supported"] = scenario.methods
				}
			}
			client := m.client(t, nil)
			for _, primed := range []bool{false, true} {
				if primed {
					if _, err := loginMock(t, m, client); err != nil {
						t.Fatal(err)
					}
				}
				authorization, err := client.Begin(context.Background(), "/", nil, "oidc:192.0.2.1")
				if err != nil {
					t.Fatal(err)
				}
				before := m.exchanges.Load()
				hook.Reset()
				result, err := client.Complete(context.Background(), authorization.State, authorization.Binding, "invalid-code", "")
				if !errors.Is(err, oidc.ErrUnavailable) || !result.AuthenticationFailed {
					t.Fatalf("primed=%v invalid grant = %+v, %v", primed, result, err)
				}
				wantRequests := int64(1)
				if !primed && scenario.name == "both post" {
					wantRequests = 2
				}
				if got := m.exchanges.Load() - before; got != wantRequests {
					t.Fatalf("primed=%v token requests = %d, want %d", primed, got, wantRequests)
				}
				requireOIDCDiagnostic(t, hook, "token", "token_response_rejected", 400, "invalid_grant", "invalid-code", authorization.Binding)
				if len(hook.AllEntries()) != 1 {
					t.Fatal("authentication retry produced duplicate diagnostics")
				}
			}
		})
	}
}

func TestOIDCClientAuthenticationRetryHonorsHTTPStatus(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		want   int64
	}{
		{"client rejection", 401, 2},
		{"busy client rejection", 429, 1},
		{"server client rejection", 503, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var requests atomic.Int64
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			}))
			defer endpoint.Close()
			m := newMockIDP(t, false, "")
			m.metadata = func(metadata map[string]any) { metadata["token_endpoint"] = endpoint.URL }
			result, err := loginMock(t, m, m.client(t, nil))
			if !errors.Is(err, oidc.ErrUnavailable) || result.AuthenticationFailed {
				t.Fatalf("unexpected token rejection = %+v, %v", result, err)
			}
			if got := requests.Load(); got != scenario.want {
				t.Fatalf("token requests = %d, want %d", got, scenario.want)
			}
		})
	}
}

func TestOIDCClientAuthenticationChoiceSurvivesDiscoveryRefresh(t *testing.T) {
	m := newMockIDP(t, false, "")
	m.style, m.cacheControl = "post", "no-store"
	client := m.client(t, nil)
	for range 2 {
		if _, err := loginMock(t, m, client); err != nil {
			t.Fatal(err)
		}
	}
	if m.exchanges.Load() != 3 || m.discoveries.Load() < 2 {
		t.Fatalf("authentication choice lost across refresh: token=%d discovery=%d", m.exchanges.Load(), m.discoveries.Load())
	}
}

func TestOIDCConcurrentClientAuthenticationKeepsCachedChoice(t *testing.T) {
	m := newMockIDP(t, false, "")
	m.style = "post"
	client := m.client(t, nil)
	var pending sync.WaitGroup
	for range 6 {
		pending.Add(1)
		go func() {
			defer pending.Done()
			if result, err := loginMock(t, m, client); err != nil || result.Identity.Subject != "Subject,A" {
				t.Errorf("concurrent login = %+v, %v", result, err)
			}
		}()
	}
	pending.Wait()
	before := m.exchanges.Load()
	if _, err := loginMock(t, m, client); err != nil {
		t.Fatal(err)
	}
	if got := m.exchanges.Load() - before; got != 1 {
		t.Fatalf("cached authentication repeated detection: %d token requests", got)
	}
}
