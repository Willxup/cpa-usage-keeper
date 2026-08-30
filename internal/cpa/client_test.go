package cpa

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

func TestBlankJSONResponseBodyCheckDoesNotAllocate(t *testing.T) {
	body := []byte(" \n\t ")

	allocs := testing.AllocsPerRun(1000, func() {
		if !isBlankJSONResponseBody(body) {
			t.Fatalf("expected whitespace-only response body to be blank")
		}
	})
	if allocs != 0 {
		t.Fatalf("expected blank response check to allocate 0 times, got %f", allocs)
	}
}

func TestFetchManagementAPIKeysSendsBearerTokenAndParsesKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAPIKeysEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api-keys":["sk-alpha", "sk-beta"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchManagementAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchManagementAPIKeys returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if string(result.Body) != `{"api-keys":["sk-alpha", "sk-beta"]}` {
		t.Fatalf("unexpected body: %s", string(result.Body))
	}
	if len(result.Payload.APIKeys) != 2 || result.Payload.APIKeys[0] != "sk-alpha" || result.Payload.APIKeys[1] != "sk-beta" {
		t.Fatalf("unexpected API keys payload: %#v", result.Payload)
	}
}

func TestFetchRequestLogByIDDownloadsFileWithBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementRequestLogByIDEndpoint+"/req-log-42" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="error-v1-responses-req-log-42.log"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("=== REQUEST INFO ===\nURL: /v1/responses\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchRequestLogByID(context.Background(), " req-log-42 ")
	if err != nil {
		t.Fatalf("FetchRequestLogByID returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if result.Filename != "error-v1-responses-req-log-42.log" {
		t.Fatalf("unexpected filename %q", result.Filename)
	}
	if result.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type %q", result.ContentType)
	}
	if string(result.Body) != "=== REQUEST INFO ===\nURL: /v1/responses\n" {
		t.Fatalf("unexpected body %q", string(result.Body))
	}
}

func TestFetchRequestLogByIDLimitsPreviewBody(t *testing.T) {
	previewLimit := int64(16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="large-request.log"`)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("0123456789abcdefX"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.fetchRequestLogByID(context.Background(), "req-large", previewLimit)

	if err != nil {
		t.Fatalf("fetchRequestLogByID returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if !result.BodyTruncated {
		t.Fatalf("expected oversized response to be marked truncated")
	}
	if len(result.Body) != int(previewLimit+1) {
		t.Fatalf("expected limited body length %d, got %d", previewLimit+1, len(result.Body))
	}
	if result.Filename != "large-request.log" {
		t.Fatalf("unexpected filename %q", result.Filename)
	}
}

func TestFetchRequestLogByIDSkipsBodyWhenContentLengthExceedsPreviewLimit(t *testing.T) {
	previewLimit := int64(16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="large-request.log"`)
		w.Header().Set("Content-Length", "17")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.fetchRequestLogByID(context.Background(), "req-large", previewLimit)

	if err != nil {
		t.Fatalf("fetchRequestLogByID returned error: %v", err)
	}
	if !result.BodyTruncated {
		t.Fatalf("expected content-length oversized response to be marked truncated")
	}
	if len(result.Body) != 0 {
		t.Fatalf("expected oversized response body not to be read, got %d bytes", len(result.Body))
	}
	if result.ContentLength != 17 {
		t.Fatalf("expected original content length 17, got %d", result.ContentLength)
	}
}

func TestFetchRequestLogByIDKeepsUnknownContentLengthWhenPreviewBodyTruncated(t *testing.T) {
	previewLimit := int64(16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="large-request.log"`)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("0123456789abcdefX"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.fetchRequestLogByID(context.Background(), "req-large", previewLimit)

	if err != nil {
		t.Fatalf("fetchRequestLogByID returned error: %v", err)
	}
	if !result.BodyTruncated {
		t.Fatalf("expected oversized response to be marked truncated")
	}
	if result.ContentLength != -1 {
		t.Fatalf("expected unknown content length to stay -1 after truncation, got %d", result.ContentLength)
	}
}

func TestOpenRequestLogByIDDoesNotTimeoutWhileStreamingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="slow-request.log"`)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte("late body"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 20*time.Millisecond, false)
	stream, err := client.OpenRequestLogByID(context.Background(), "req-slow")
	if err != nil {
		t.Fatalf("OpenRequestLogByID returned error before streaming body: %v", err)
	}
	defer stream.Body.Close()

	body, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("expected stream body read to outlive client request timeout, got %v", err)
	}
	if string(body) != "late body" {
		t.Fatalf("unexpected stream body %q", string(body))
	}
}

func TestOpenRequestLogByIDTimesOutStalledStreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="stalled-request.log"`)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	client.streamIdleTimeout = 20 * time.Millisecond
	stream, err := client.OpenRequestLogByID(context.Background(), "req-stalled")
	if err != nil {
		t.Fatalf("OpenRequestLogByID returned error before streaming body: %v", err)
	}
	defer stream.Body.Close()

	startedAt := time.Now()
	_, err = io.ReadAll(stream.Body)
	if err == nil {
		t.Fatalf("expected stalled stream body read to fail")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
	if time.Since(startedAt) > time.Second {
		t.Fatalf("expected stalled stream body to fail quickly, took %s", time.Since(startedAt))
	}
}

func TestIdleTimeoutReadCloserNilReceiver(t *testing.T) {
	var reader *idleTimeoutReadCloser

	n, err := reader.Read(make([]byte, 1))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("expected nil reader to return EOF, got n=%d err=%v", n, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("expected nil reader close to be a no-op, got %v", err)
	}
}

func TestFetchManagementAPIKeysAllowsEmptyArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api-keys":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchManagementAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchManagementAPIKeys returned error: %v", err)
	}
	if result.Payload.APIKeys == nil || len(result.Payload.APIKeys) != 0 {
		t.Fatalf("expected empty API key list, got %#v", result.Payload.APIKeys)
	}
}

func TestFetchManagementAPIKeysReportsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.FetchManagementAPIKeys(context.Background())
	if err == nil || !strings.Contains(err.Error(), "management api keys request returned status 502") {
		t.Fatalf("expected management request failure, got %v", err)
	}
}

func TestFetchManagementAPIKeysRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad-json}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.FetchManagementAPIKeys(context.Background())
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
}

func TestFetchAuthFilesParsesSyncMetadataFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAuthFilesEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[{"auth_index":"codex-auth","name":"codex-user.json","path":"/data/auths/codex-user.json","type":"codex","prefix":"team","priority":7,"disabled":false,"note":"primary auth"},{"auth_index":"gemini-auth","type":"gemini"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("FetchAuthFiles returned error: %v", err)
	}
	if len(result.Payload.Files) != 2 {
		t.Fatalf("expected two auth files, got %#v", result.Payload.Files)
	}
	file := result.Payload.Files[0]
	if file.Prefix != "team" || file.Priority == nil || *file.Priority != 7 || file.Disabled == nil || *file.Disabled || file.Note == nil || *file.Note != "primary auth" {
		t.Fatalf("expected sync metadata fields to decode, got %+v", file)
	}
	if file.Name != "codex-user.json" || file.Path != "/data/auths/codex-user.json" {
		t.Fatalf("expected auth file name and path to decode, got %+v", file)
	}
	missing := result.Payload.Files[1]
	if missing.Priority != nil || missing.Disabled != nil || missing.Note != nil || missing.Prefix != "" {
		t.Fatalf("expected missing sync metadata fields to stay empty, got %+v", missing)
	}
}

func TestFetchAuthFilesParsesCodexIDTokenFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAuthFilesEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[{"auth_index":"codex-auth","type":"codex","id_token":{"chatgpt_account_id":"acct_123","chatgpt_subscription_active_start":"2026-05-01T00:00:00Z","chatgpt_subscription_active_until":"2026-06-01T00:00:00Z","plan_type":"team"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("FetchAuthFiles returned error: %v", err)
	}
	if len(result.Payload.Files) != 1 {
		t.Fatalf("expected one auth file, got %#v", result.Payload.Files)
	}
	file := result.Payload.Files[0]
	if file.IDToken == nil {
		t.Fatalf("expected id_token to decode, got %+v", file)
	}
	if file.IDToken.AccountID == nil || *file.IDToken.AccountID != "acct_123" {
		t.Fatalf("expected account id to decode, got %+v", file.IDToken)
	}
	if file.IDToken.ActiveStart == nil || !file.IDToken.ActiveStart.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected active start to decode, got %+v", file.IDToken.ActiveStart)
	}
	if file.IDToken.ActiveUntil == nil || !file.IDToken.ActiveUntil.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected active until to decode, got %+v", file.IDToken.ActiveUntil)
	}
	if file.IDToken.PlanType == nil || *file.IDToken.PlanType != "team" {
		t.Fatalf("expected plan type to decode, got %+v", file.IDToken.PlanType)
	}
}

func TestFetchAuthFilesEnrichesSidecarAccountAndTeamMetadata(t *testing.T) {
	const sharedEmail = "same-user@example.invalid"
	var downloadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Errorf("expected management Authorization header, got %q", got)
		}
		switch r.URL.Path {
		case cpaManagementAuthFilesEndpoint:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[
				{"name":"codex-account-a-same-user@example.invalid-team-agent-identity.json","auth_index":"agent-auth-a","type":"codex","email":"` + sharedEmail + `"},
				{"name":"codex-account-b-same-user@example.invalid-team-agent-identity.json","auth_index":"agent-auth-b","type":"codex","email":"` + sharedEmail + `"},
				{"name":"codex-native.json","auth_index":"native-auth","type":"codex","email":"native@example.invalid"}
			]}`))
		case cpaManagementAuthFilesDownloadEndpoint:
			downloadCalls++
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("name") {
			case "codex-account-a-same-user@example.invalid-team-agent-identity.json":
				_, _ = w.Write([]byte(`{"type":"codex","auth_mode":"agent_identity_sidecar","credential_kind":"agent_identity","agent_identity_id":"agent-aabb0011","email":"` + sharedEmail + `","account_id":"team-account-a","chatgpt_user_id":"chatgpt-user-a","plan_type":"team","access_token":"cais_secret_a_do_not_return"}`))
			case "codex-account-b-same-user@example.invalid-team-agent-identity.json":
				_, _ = w.Write([]byte(`{"type":"codex","auth_mode":"agent_identity_sidecar","credential_kind":"personal_access_token","agent_identity_id":"agent-ccdd2233","email":"` + sharedEmail + `","account_id":"team-account-b","chatgpt_user_id":"chatgpt-user-b","plan_type":"team","access_token":"cais_secret_b_do_not_return"}`))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("FetchAuthFiles returned error: %v", err)
	}
	if len(result.Payload.Files) != 3 {
		t.Fatalf("expected three auth files, got %#v", result.Payload.Files)
	}
	if downloadCalls != 2 {
		t.Fatalf("expected only sidecar candidates to be downloaded, got %d calls", downloadCalls)
	}
	if bytes.Contains(result.Body, []byte("cais_secret_a_do_not_return")) || bytes.Contains(result.Body, []byte("cais_secret_b_do_not_return")) {
		t.Fatalf("auth-files response body contains a sidecar credential")
	}

	filesByIndex := make(map[string]authfiles.AuthFile, len(result.Payload.Files))
	for _, file := range result.Payload.Files {
		filesByIndex[file.AuthIndex] = file
	}
	first := filesByIndex["agent-auth-a"]
	second := filesByIndex["agent-auth-b"]
	if first.AccountID != "team-account-a" || first.ChatGPTUserID != "chatgpt-user-a" || first.PlanType != "team" || first.AuthMode != "agent_identity_sidecar" {
		t.Fatalf("first sidecar metadata = %+v", first)
	}
	if second.AccountID != "team-account-b" || second.ChatGPTUserID != "chatgpt-user-b" || second.PlanType != "team" || second.CredentialKind != "personal_access_token" {
		t.Fatalf("second sidecar metadata = %+v", second)
	}
	if first.Email != sharedEmail || second.Email != sharedEmail || first.AccountID == second.AccountID {
		t.Fatalf("same-email sidecars were not kept distinct: first=%+v second=%+v", first, second)
	}
	if native := filesByIndex["native-auth"]; native.AccountID != "" || native.PlanType != "" || native.AuthMode != "" {
		t.Fatalf("native OAuth file was unexpectedly enriched: %+v", native)
	}
}

func TestClassifyCodexAuthFileRejectsUnknownShape(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want codexAuthFileClassification
	}{
		{name: "native oauth", raw: `{"type":"codex","access_token":"oauth-access","refresh_token":"oauth-refresh","id_token":"header.payload.signature"}`, want: codexAuthFileNative},
		{name: "sidecar", raw: `{"type":"codex","auth_mode":"agent_identity_sidecar","agent_identity_id":"agent-aabb0011","access_token":"cais_fake_for_test_only"}`, want: codexAuthFileSidecar},
		{name: "unknown codex", raw: `{"type":"codex","access_token":"opaque-but-unclassified"}`, want: codexAuthFileUnknown},
		{name: "plugin type without marker", raw: `{"type":"codex-agent-identity","access_token":"opaque-but-unclassified"}`, want: codexAuthFileUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyCodexAuthFile([]byte(test.raw)); got != test.want {
				t.Fatalf("classifyCodexAuthFile() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFetchAuthFilesUsesPathBasenameAndNestedCodexMetadata(t *testing.T) {
	var downloadNames []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Errorf("expected management Authorization header, got %q", got)
		}
		switch r.URL.Path {
		case cpaManagementAuthFilesEndpoint:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"name":"auth-record.json","path":"/var/lib/cpa/auths/codex-agent-identity-team.json","auth_index":"team-auth","type":"codex-agent-identity","email":"team-user@example.invalid"}]}`))
		case cpaManagementAuthFilesDownloadEndpoint:
			name := r.URL.Query().Get("name")
			downloadNames = append(downloadNames, name)
			if name == "auth-record.json" {
				http.NotFound(w, r)
				return
			}
			if name != "codex-agent-identity-team.json" {
				t.Errorf("unexpected download candidate %q", name)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metadata":{"type":"codex-agent-identity","auth_mode":"agent_identity_sidecar","agent_identity_id":"agent-aabb0011","account_id":"team-from-path","plan_type":"team","id_token":{"chatgpt_account_id":"team-from-id-token"}},"attributes":{"chatgpt_user_id":"chatgpt-user-from-attributes"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("FetchAuthFiles returned error: %v", err)
	}
	if len(downloadNames) != 2 || downloadNames[0] != "auth-record.json" || downloadNames[1] != "codex-agent-identity-team.json" {
		t.Fatalf("expected safe name/path candidate order, got %#v", downloadNames)
	}
	if len(result.Payload.Files) != 1 {
		t.Fatalf("expected one auth file, got %#v", result.Payload.Files)
	}
	file := result.Payload.Files[0]
	if file.Type != "codex-agent-identity" || file.AuthMode != "agent_identity_sidecar" || file.AgentIdentityID != "agent-aabb0011" {
		t.Fatalf("nested sidecar classification metadata = %+v", file)
	}
	if file.AccountID != "team-from-path" || file.ChatGPTAccountID != "team-from-id-token" || file.PlanType != "team" || file.ChatGPTUserID != "chatgpt-user-from-attributes" {
		t.Fatalf("nested sidecar routing metadata = %+v", file)
	}
	if file.IDToken != nil {
		t.Fatalf("downloaded sidecar id_token claims must not be promoted to a raw credential field: %+v", file.IDToken)
	}
}

func TestAuthFileDownloadNamesRejectTraversalAndUseJSONBasenames(t *testing.T) {
	if got := authFileDownloadNames("../secret.json", `C:\\data\\safe.json`); len(got) != 1 || got[0] != "safe.json" {
		t.Fatalf("unexpected sanitized auth file candidates: %#v", got)
	}
	if got := authFileDownloadNames("auth-record.txt", "/data/auths/real.json"); len(got) != 1 || got[0] != "real.json" {
		t.Fatalf("expected invalid name to fall back to path basename, got %#v", got)
	}
	if got := authFileDownloadNames("nested/auth.json", "/data/auths/auth.json"); len(got) != 1 || got[0] != "auth.json" {
		t.Fatalf("expected duplicate basenames to be deduplicated, got %#v", got)
	}
	if got := authFileDownloadNames("../secret.json", "../also-secret.json"); len(got) != 0 {
		t.Fatalf("expected traversal candidates to be rejected, got %#v", got)
	}
}

func TestDownloadAuthFileRawLimitsResponseWithoutReturningCredentialBody(t *testing.T) {
	const sentinel = "cais_secret_should_not_escape"
	body := strings.Repeat("x", int(authFileDownloadMaxBytes)-len(sentinel)+1) + sentinel
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAuthFilesDownloadEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, exists, err := client.downloadAuthFileRaw(context.Background(), "oversized.json", "")
	if err == nil {
		t.Fatal("expected oversized auth file response error")
	}
	if exists {
		t.Fatal("oversized auth file must not be reported as existing")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("oversized response leaked credential content in error: %v", err)
	}
}

func TestUpdateAuthFileStatusPatchesManagementEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("expected PATCH method, got %s", r.Method)
		}
		if r.URL.Path != cpaManagementAuthFilesStatusEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected JSON content type, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["name"] != "codex-user.json" || body["disabled"] != true {
			t.Fatalf("unexpected status request body: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	if err := client.UpdateAuthFileStatus(context.Background(), "codex-user.json", true); err != nil {
		t.Fatalf("UpdateAuthFileStatus returned error: %v", err)
	}
}

func TestDeleteAuthFilesSendsNamesToManagementEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected DELETE method, got %s", r.Method)
		}
		if r.URL.Path != cpaManagementAuthFilesEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected JSON content type, got %q", got)
		}

		var body map[string][]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if strings.Join(body["names"], ",") != "codex-a.json,codex-b.json" {
			t.Fatalf("unexpected delete request body: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	if err := client.DeleteAuthFiles(context.Background(), []string{"codex-a.json", "codex-b.json"}); err != nil {
		t.Fatalf("DeleteAuthFiles returned error: %v", err)
	}
}

func TestCallManagementAPIPostsWrappedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != cpaManagementAPICallEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected JSON content type, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["authIndex"] != "codex-auth" || body["method"] != "GET" || body["url"] != "https://provider.example.com/usage" {
			t.Fatalf("unexpected api-call body: %#v", body)
		}
		header, ok := body["header"].(map[string]any)
		if !ok || header["Chatgpt-Account-Id"] != "acct_123" {
			t.Fatalf("unexpected api-call header body: %#v", body["header"])
		}
		data, ok := body["data"].(string)
		if !ok {
			t.Fatalf("expected api-call data to be JSON string, got %#v", body["data"])
		}
		var decodedData map[string]string
		if err := json.Unmarshal([]byte(data), &decodedData); err != nil {
			t.Fatalf("decode api-call data string: %v", err)
		}
		if decodedData["project"] != "project-123" {
			t.Fatalf("unexpected api-call data body: %#v", decodedData)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"statusCode":200,"bodyText":"ok","body":{"remaining":10}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.CallManagementAPI(context.Background(), apicall.Request{
		AuthIndex: "codex-auth",
		Method:    "GET",
		URL:       "https://provider.example.com/usage",
		Header:    map[string]string{"Chatgpt-Account-Id": "acct_123"},
		Data:      map[string]string{"project": "project-123"},
	})
	if err != nil {
		t.Fatalf("CallManagementAPI returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK || result.BodyText != "ok" || string(result.Body) != `{"remaining":10}` {
		t.Fatalf("unexpected api-call response: %+v", result)
	}
}

func TestCallManagementAPIParsesSnakeCaseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAPICallEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":201,"body_text":"created","body":{"ok":true}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.CallManagementAPI(context.Background(), apicall.Request{AuthIndex: "kimi-auth", Method: "GET", URL: "https://provider.example.com/usage"})
	if err != nil {
		t.Fatalf("CallManagementAPI returned error: %v", err)
	}
	if result.StatusCode != http.StatusCreated || result.BodyText != "created" || string(result.Body) != `{"ok":true}` {
		t.Fatalf("unexpected snake case api-call response: %+v", result)
	}
}

func TestCallManagementAPICodexQuotaUsesPluginBridge(t *testing.T) {
	tests := []struct {
		name   string
		method string
		url    string
		data   any
	}{
		{
			name:   "usage",
			method: http.MethodGet,
			url:    "https://chatgpt.com/backend-api/wham/usage",
		},
		{
			name:   "reset credit details",
			method: http.MethodGet,
			url:    "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits",
		},
		{
			name:   "reset credit consume",
			method: http.MethodPost,
			url:    "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume",
			data:   map[string]string{"redeem_request_id": "redeem-test"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST method, got %s", r.Method)
				}
				if r.URL.Path != cpaManagementCodexAgentIdentityAPICallEndpoint {
					t.Errorf("expected plugin bridge path %q, got %q", cpaManagementCodexAgentIdentityAPICallEndpoint, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
					t.Errorf("expected management Authorization header, got %q", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("expected JSON content type, got %q", got)
				}

				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode bridge request body: %v", err)
				}
				if body["authIndex"] != "codex-auth" || body["method"] != test.method || body["url"] != test.url {
					t.Errorf("unexpected bridge request body: %#v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"statusCode":200,"bodyText":"bridge-ok","body":{"bridge":true}}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := client.CallManagementAPI(context.Background(), apicall.Request{
				AuthIndex: "codex-auth",
				Method:    test.method,
				URL:       test.url,
				Header:    map[string]string{"Chatgpt-Account-Id": "acct-test"},
				Data:      test.data,
			})
			if err != nil {
				t.Fatalf("CallManagementAPI returned error: %v", err)
			}
			if result.StatusCode != http.StatusOK || result.BodyText != "bridge-ok" || string(result.Body) != `{"bridge":true}` {
				t.Fatalf("unexpected bridge response: %+v", result)
			}
		})
	}
}

func TestIsCodexQuotaRequestMatchesOnlySupportedEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		url     string
		expects bool
	}{
		{name: "usage", method: http.MethodGet, url: "https://chatgpt.com/backend-api/wham/usage", expects: true},
		{name: "reset details", method: http.MethodGet, url: "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits", expects: true},
		{name: "reset consume", method: http.MethodPost, url: "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume", expects: true},
		{name: "lowercase method", method: "get", url: "https://chatgpt.com/backend-api/wham/usage", expects: true},
		{name: "wrong method", method: http.MethodPost, url: "https://chatgpt.com/backend-api/wham/usage", expects: false},
		{name: "query string", method: http.MethodGet, url: "https://chatgpt.com/backend-api/wham/usage?account=1", expects: false},
		{name: "fragment", method: http.MethodGet, url: "https://chatgpt.com/backend-api/wham/usage#fragment", expects: false},
		{name: "trailing slash", method: http.MethodGet, url: "https://chatgpt.com/backend-api/wham/usage/", expects: false},
		{name: "wrong scheme", method: http.MethodGet, url: "http://chatgpt.com/backend-api/wham/usage", expects: false},
		{name: "wrong host", method: http.MethodGet, url: "https://chat.openai.com/backend-api/wham/usage", expects: false},
		{name: "host suffix", method: http.MethodGet, url: "https://chatgpt.com.evil.example/backend-api/wham/usage", expects: false},
		{name: "explicit port", method: http.MethodGet, url: "https://chatgpt.com:443/backend-api/wham/usage", expects: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isCodexQuotaRequest(apicall.Request{Method: test.method, URL: test.url})
			if got != test.expects {
				t.Fatalf("isCodexQuotaRequest(%q, %q) = %v, want %v", test.method, test.url, got, test.expects)
			}
		})
	}
}

func TestCallManagementAPIFallsBackToNativeForKnownOAuthWhenBridgeIsMissing(t *testing.T) {
	var bridgeBody, nativeBody []byte
	var bridgeCalls, nativeCalls, listCalls, downloadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Errorf("expected management Authorization header, got %q", got)
		}
		switch r.URL.Path {
		case cpaManagementCodexAgentIdentityAPICallEndpoint:
			bridgeCalls++
			bridgeBody, _ = io.ReadAll(r.Body)
			http.NotFound(w, r)
		case cpaManagementAuthFilesEndpoint:
			listCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"name":"codex-native-agent-identity.json","auth_index":"oauth-auth","type":"codex"}]}`))
		case cpaManagementAuthFilesDownloadEndpoint:
			downloadCalls++
			if got := r.URL.Query().Get("name"); got != "codex-native-agent-identity.json" {
				t.Errorf("unexpected auth file download name %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"codex","access_token":"oauth-token","refresh_token":"oauth-refresh-token","id_token":"header.payload.signature"}`))
		case cpaManagementAPICallEndpoint:
			nativeCalls++
			nativeBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"statusCode":200,"bodyText":"native-ok","body":{"oauth":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.CallManagementAPI(context.Background(), apicall.Request{
		AuthIndex: "oauth-auth",
		Method:    http.MethodGet,
		URL:       "https://chatgpt.com/backend-api/wham/usage",
		Header:    map[string]string{"User-Agent": "keeper-test"},
	})
	if err != nil {
		t.Fatalf("CallManagementAPI returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK || result.BodyText != "native-ok" || string(result.Body) != `{"oauth":true}` {
		t.Fatalf("unexpected native fallback response: %+v", result)
	}
	if bridgeCalls != 1 || nativeCalls != 1 || listCalls != 1 || downloadCalls != 1 {
		t.Fatalf("unexpected fallback calls: bridge=%d native=%d list=%d download=%d", bridgeCalls, nativeCalls, listCalls, downloadCalls)
	}
	if len(bridgeBody) == 0 || !bytes.Equal(bridgeBody, nativeBody) {
		t.Fatalf("bridge and native request bodies differ: bridge=%s native=%s", bridgeBody, nativeBody)
	}
}

func TestCallManagementAPINeverFallsBackForSidecarAuthWhenBridgeIsMissing(t *testing.T) {
	var nativeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementCodexAgentIdentityAPICallEndpoint:
			http.NotFound(w, r)
		case cpaManagementAuthFilesEndpoint:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"name":"codex-agent.json","auth_index":"agent-auth","type":"codex"}]}`))
		case cpaManagementAuthFilesDownloadEndpoint:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"codex","auth_mode":"agent_identity_sidecar","agent_identity_id":"agent-aabb0011","access_token":"cais_fake_for_test_only"}`))
		case cpaManagementAPICallEndpoint:
			nativeCalls++
			http.Error(w, "native route must not receive sidecar auth", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.CallManagementAPI(context.Background(), apicall.Request{
		AuthIndex: "agent-auth",
		Method:    http.MethodGet,
		URL:       "https://chatgpt.com/backend-api/wham/usage",
	})
	if err == nil {
		t.Fatal("expected bridge-unavailable error")
	}
	if !strings.Contains(err.Error(), "install or enable the Codex Agent Identity plugin") {
		t.Fatalf("unexpected bridge-unavailable error: %v", err)
	}
	if strings.Contains(err.Error(), "cais_fake_for_test_only") {
		t.Fatalf("error leaked an opaque credential: %v", err)
	}
	if nativeCalls != 0 {
		t.Fatalf("native route received a sidecar request %d time(s)", nativeCalls)
	}
}

func TestFetchUsageQueueUsesManagementEndpointAndParsesMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementUsageQueueEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Fatalf("expected count=2, got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"request_id":"req-1"},{"request_id":"req-2"}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchUsageQueue(context.Background(), 2)
	if err != nil {
		t.Fatalf("FetchUsageQueue returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if len(result.Payload) != 2 || string(result.Payload[0]) != `{"request_id":"req-1"}` || string(result.Payload[1]) != `{"request_id":"req-2"}` {
		t.Fatalf("unexpected usage queue payload: %#v", result.Payload)
	}
}

func TestFetchUsageQueueRejectsNonPositiveCount(t *testing.T) {
	client := NewClient("https://cpa.example.com", "management-secret", 2*time.Second, false)
	if _, err := client.FetchUsageQueue(context.Background(), 0); err == nil {
		t.Fatal("expected invalid count error")
	}
}

func TestFetchModelsUsesExternalAPIKeyAndParsesOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementAPIKeysEndpoint:
			if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
				t.Fatalf("expected management Authorization header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"api-keys":["", "   ", "normal-api-key"]}`))
		case cpaModelsEndpoint:
			if got := r.Header.Get("Authorization"); got != "Bearer normal-api-key" {
				t.Fatalf("expected normal API Authorization header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"claude-sonnet","object":"model","created":123,"owned_by":"anthropic"}]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if len(result.Payload.Data) != 1 || result.Payload.Data[0].ID != "claude-sonnet" {
		t.Fatalf("unexpected models payload: %#v", result.Payload)
	}
}

func TestFetchModelsRejectsMissingManagementAPIKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementAPIKeysEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"api-keys":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing management API keys error")
	}
}

func TestFetchModelsDoesNotUseProviderEndpointsWhenCPAManagementAPIKeysAreMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":[]}`))
		case cpaManagementClaudeAPIKeyEndpoint, cpaManagementCodexAPIKeyEndpoint, cpaManagementOpenAICompatibilityEndpoint, cpaModelsEndpoint:
			t.Fatalf("FetchModels should not request %s when CPA management API keys are missing", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing CPA management API keys error")
	}
}

func TestFetchModelsHandlesModelNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			http.Error(w, `{"error":"unavailable"}`, http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected non-success status error")
	}
}

func TestFetchModelsRejectsRedirectStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			w.WriteHeader(http.StatusFound)
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected redirect status error")
	}
}

func TestFetchModelsRejectsInvalidModelsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestNewClientTLSSkipVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api-keys":["test-key"]}`))
	}))
	defer server.Close()

	t.Run("fails without skip verify", func(t *testing.T) {
		client := NewClient(server.URL, "management-secret", 2*time.Second, false)
		_, err := client.FetchManagementAPIKeys(context.Background())
		if err == nil {
			t.Fatal("expected TLS certificate error, got nil")
		}
		var unknownAuth x509.UnknownAuthorityError
		if !errors.As(err, &unknownAuth) {
			t.Fatalf("expected x509.UnknownAuthorityError, got: %T: %v", err, err)
		}
	})

	t.Run("succeeds with skip verify", func(t *testing.T) {
		client := NewClient(server.URL, "management-secret", 2*time.Second, true)
		result, err := client.FetchManagementAPIKeys(context.Background())
		if err != nil {
			t.Fatalf("expected success with tlsSkipVerify=true, got error: %v", err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", result.StatusCode)
		}
	})
}
