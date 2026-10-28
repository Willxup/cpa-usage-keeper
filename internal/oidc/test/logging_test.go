package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/oidc"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func captureOIDCLogs(t *testing.T) *logtest.Hook {
	t.Helper()
	logger := logrus.StandardLogger()
	hooks, level := logger.ReplaceHooks(make(logrus.LevelHooks)), logger.GetLevel()
	logger.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() { logger.ReplaceHooks(hooks); logger.SetLevel(level) })
	return logtest.NewGlobal()
}

func requireOIDCDiagnostic(t *testing.T, hook *logtest.Hook, stage, reason string, status int, oauthError string, secrets ...string) {
	t.Helper()
	found := false
	for _, entry := range hook.AllEntries() {
		data, err := json.Marshal(entry.Data)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(data)+entry.Message, secret) {
				t.Fatalf("diagnostic contains sensitive value %q", secret)
			}
		}
		if entry.Data["stage"] != stage {
			continue
		}
		found = true
		if reason != "" && entry.Data["reason"] != reason || status != 0 && entry.Data["http_status"] != status || oauthError != "" && entry.Data["oauth_error"] != oauthError {
			t.Fatalf("unexpected diagnostic: %+v", entry.Data)
		}
	}
	if !found {
		t.Fatalf("missing %s diagnostic", stage)
	}
}

func TestOIDCTokenDiagnosticsDoNotExposeProviderPayload(t *testing.T) {
	for _, code := range []string{"invalid_grant", "private-error-value"} {
		t.Run(code, func(t *testing.T) {
			hook := captureOIDCLogs(t)
			failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "sensitive-description", "error_uri": "https://example.com/private-uri", "access_token": "private-token"})
			}))
			defer failure.Close()
			m := newMockIDP(t, false, "")
			m.metadata = func(metadata map[string]any) { metadata["token_endpoint"] = failure.URL + "?secret=query-secret" }
			client := m.client(t, nil)
			authorization, err := client.Begin(context.Background(), "/", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), ""); !errors.Is(err, oidc.ErrUnavailable) {
				t.Fatalf("frontend error classification changed: %v", err)
			}
			wantCode := code
			if code == "private-error-value" {
				wantCode = "unrecognized_error"
			}
			requireOIDCDiagnostic(t, hook, "token", "token_response_rejected", 400, wantCode,
				"sensitive-description", "private-uri", "private-token", "query-secret", "private-error-value", authorization.State, authorization.Binding)
		})
	}
}

func TestOIDCAuthorizationDiagnosticsRequireBrowserBinding(t *testing.T) {
	hook := captureOIDCLogs(t)
	m := newMockIDP(t, false, "")
	client := m.client(t, nil)
	authorization, err := client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hook.Reset()
	if _, err := client.Complete(context.Background(), authorization.State, "wrong-browser", "", "private-error-value"); !errors.Is(err, oidc.ErrInvalid) {
		t.Fatal(err)
	}
	if len(hook.AllEntries()) != 0 {
		t.Fatal("unbound callback was treated as a provider response")
	}
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, "", "private-error-value"); !errors.Is(err, oidc.ErrInvalid) {
		t.Fatal(err)
	}
	requireOIDCDiagnostic(t, hook, "authorization", "", 0, "unrecognized_error", "private-error-value", authorization.State, authorization.Binding)
	authorization, err = client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hook.Reset()
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, "", "access_denied"); !errors.Is(err, oidc.ErrCanceled) {
		t.Fatal(err)
	}
	if len(hook.AllEntries()) != 0 {
		t.Fatal("canceling authorization produced a diagnostic")
	}
}

func TestOIDCJWKSAndUserInfoDiagnosticsPreserveLoginBehavior(t *testing.T) {
	for _, endpoint := range []string{"jwks_uri", "userinfo_endpoint"} {
		t.Run(endpoint, func(t *testing.T) {
			hook := captureOIDCLogs(t)
			failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "sensitive-upstream-body", 503)
			}))
			defer failure.Close()
			m := newMockIDP(t, false, "")
			m.metadata = func(metadata map[string]any) { metadata[endpoint] = failure.URL + "?token=private-token" }
			m.claims = func(claims map[string]any) { delete(claims, "name"); claims["preferred_username"] = "fallback" }
			result, err := loginMock(t, m, m.client(t, nil))
			wantFailures := 1
			if endpoint == "userinfo_endpoint" {
				wantFailures = 0
				if err != nil || result.Identity.Subject != "Subject,A" || result.Identity.PreferredUsername != "fallback" {
					t.Fatalf("optional UserInfo failure blocked login: %+v, %v", result, err)
				}
			} else if !errors.Is(err, oidc.ErrInvalid) {
				t.Fatalf("JWKS error changed: %v", err)
			} else {
				requireOIDCDiagnostic(t, hook, "id_token", "verification_failed", 0, "", "sensitive-upstream-body", "private-token")
			}
			failures := 0
			for _, entry := range hook.AllEntries() {
				if entry.Data["stage"] != nil {
					failures++
				}
			}
			if failures != wantFailures {
				t.Fatalf("diagnostic count = %d, want %d", failures, wantFailures)
			}
		})
	}
}

type canceledTokenTransport struct {
	base http.RoundTripper
}

func (t canceledTokenTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/token" {
		return nil, context.Canceled
	}
	return t.base.RoundTrip(request)
}

func TestOIDCCanceledTokenRequestDoesNotLogAnUpstreamFailure(t *testing.T) {
	hook := captureOIDCLogs(t)
	m := newMockIDP(t, false, "")
	client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback"}, &http.Client{Transport: canceledTokenTransport{base: http.DefaultTransport}})
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := client.Begin(context.Background(), "/", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hook.Reset()
	if _, err := client.Complete(context.Background(), authorization.State, authorization.Binding, m.code(t, authorization), ""); !errors.Is(err, oidc.ErrUnavailable) {
		t.Fatalf("frontend error classification changed: %v", err)
	}
	if len(hook.AllEntries()) != 0 {
		t.Fatal("canceled request produced an upstream failure log")
	}
}

func TestOIDCCanceledJWKSRequestDoesNotLogAnUpstreamFailure(t *testing.T) {
	hook := captureOIDCLogs(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer endpoint.Close()
	defer close(release)
	m := newMockIDP(t, false, "")
	m.metadata = func(metadata map[string]any) { metadata["jwks_uri"] = endpoint.URL }
	client := m.client(t, nil)
	authorization, err := client.Begin(context.Background(), "/", nil, "oidc:192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	code := m.code(t, authorization)
	hook.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result oidc.LoginResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := client.Complete(ctx, authorization.State, authorization.Binding, code, "")
		done <- outcome{result, err}
	}()
	// 等取钥请求真正开始后再模拟浏览器中断，确保覆盖验签阶段而非 Token 请求取消。
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("JWKS request did not begin")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, oidc.ErrInvalid) || got.result.AuthenticationFailed {
			t.Fatalf("canceled verification = %+v, %v", got.result, got.err)
		}
		if len(hook.AllEntries()) != 0 {
			t.Fatal("canceled JWKS request produced an upstream failure log")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not unblock verification")
	}
}

type pendingTokenBody struct {
	ctx     context.Context
	entered chan struct{}
}

func (b *pendingTokenBody) Read(_ []byte) (int, error) {
	b.entered <- struct{}{}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *pendingTokenBody) Close() error { return nil }

type pendingTokenBodyTransport struct{ entered chan struct{} }

func (t pendingTokenBodyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/token" {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: &pendingTokenBody{ctx: request.Context(), entered: t.entered}, Request: request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestOIDCTokenBodyCancellationIsQuietButTimeoutStillLogs(t *testing.T) {
	for _, scenario := range []string{"cancel", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			hook := captureOIDCLogs(t)
			m := newMockIDP(t, false, "")
			entered := make(chan struct{}, 1)
			client, err := oidc.NewClient(oidc.Config{Issuer: m.issuer, ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example/api/v1/auth/oidc/callback"}, &http.Client{Transport: pendingTokenBodyTransport{entered: entered}})
			if err != nil {
				t.Fatal(err)
			}
			authorization, err := client.Begin(context.Background(), "/", nil, "oidc:192.0.2.1")
			if err != nil {
				t.Fatal(err)
			}
			code := m.code(t, authorization)
			hook.Reset()
			var ctx context.Context
			var cancel context.CancelFunc
			if scenario == "timeout" {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			type outcome struct {
				result oidc.LoginResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := client.Complete(ctx, authorization.State, authorization.Binding, code, "")
				done <- outcome{result, err}
			}()
			// 正文开始读取后才中断，覆盖依赖把读取错误转成文本、丢失取消类型的路径。
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("token response body was not read")
			}
			if scenario == "cancel" {
				cancel()
			}
			select {
			case got := <-done:
				if !errors.Is(got.err, oidc.ErrUnavailable) || got.result.AuthenticationFailed {
					t.Fatalf("interrupted token body = %+v, %v", got.result, got.err)
				}
				wantLogs := 0
				if scenario == "timeout" {
					wantLogs = 1
					requireOIDCDiagnostic(t, hook, "token", "", 0, "", authorization.Binding, code)
				}
				if got := len(hook.AllEntries()); got != wantLogs {
					t.Fatalf("diagnostics = %d, want %d", got, wantLogs)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("context completion did not unblock token exchange")
			}
		})
	}
}
