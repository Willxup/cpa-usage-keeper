package cpa

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/response"
)

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

func TestProviderMetadataFetchersUseDedicatedEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		fetch    func(context.Context, *Client) (*response.ProviderKeyConfigResult, error)
		response string
	}{
		{
			name: "gemini",
			path: cpaManagementGeminiAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchGeminiAPIKeys(ctx)
			},
			response: `[{"apiKey":"gemini-key","prefix":"gemini-prefix","name":"Gemini","auth-index":"gemini-auth-index"}]`,
		},
		{
			name: "claude",
			path: cpaManagementClaudeAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchClaudeAPIKeys(ctx)
			},
			response: `[{"api-key":"claude-key","prefix":"claude-prefix","name":"Claude","auth_index":"claude-auth-index"}]`,
		},
		{
			name: "codex",
			path: cpaManagementCodexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchCodexAPIKeys(ctx)
			},
			response: `[{"key":"codex-key","prefix":"codex-prefix","name":"Codex","authIndex":"codex-auth-index"}]`,
		},
		{
			name: "vertex",
			path: cpaManagementVertexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchVertexAPIKeys(ctx)
			},
			response: `[{"apiKey":"vertex-key","prefix":"vertex-prefix","name":"Vertex","auth-index":"vertex-auth-index"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
					t.Fatalf("expected management Authorization header, got %q", got)
				}
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := tt.fetch(context.Background(), client)
			if err != nil {
				t.Fatalf("fetch returned error: %v", err)
			}
			if result.StatusCode != http.StatusOK || len(result.Body) == 0 {
				t.Fatalf("unexpected result metadata: %+v", result)
			}
			if len(result.Payload) != 1 || result.Payload[0].APIKey == "" || result.Payload[0].Prefix == "" || result.Payload[0].Name == "" || result.Payload[0].AuthIndex == "" {
				t.Fatalf("unexpected provider payload: %#v", result.Payload)
			}
		})
	}
}

func TestProviderMetadataFetchersParseWrappedEndpointResponses(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		fetch    func(context.Context, *Client) (*response.ProviderKeyConfigResult, error)
		response string
	}{
		{
			name: "gemini",
			path: cpaManagementGeminiAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchGeminiAPIKeys(ctx)
			},
			response: `{"gemini-api-key":[{"apiKey":"gemini-key","prefix":"gemini-prefix","name":"Gemini","auth-index":"gemini-auth-index"}]}`,
		},
		{
			name: "claude",
			path: cpaManagementClaudeAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchClaudeAPIKeys(ctx)
			},
			response: `{"claude-api-key":[{"api-key":"claude-key","prefix":"claude-prefix","name":"Claude","auth_index":"claude-auth-index"}]}`,
		},
		{
			name: "codex",
			path: cpaManagementCodexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchCodexAPIKeys(ctx)
			},
			response: `{"codex-api-key":[{"key":"codex-key","prefix":"codex-prefix","name":"Codex","authIndex":"codex-auth-index"}]}`,
		},
		{
			name: "vertex",
			path: cpaManagementVertexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*response.ProviderKeyConfigResult, error) {
				return client.FetchVertexAPIKeys(ctx)
			},
			response: `{"vertex-api-key":[{"apiKey":"vertex-key","prefix":"vertex-prefix","name":"Vertex","auth-index":"vertex-auth-index"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := tt.fetch(context.Background(), client)
			if err != nil {
				t.Fatalf("fetch returned error: %v", err)
			}
			if len(result.Payload) != 1 || result.Payload[0].APIKey == "" || result.Payload[0].Prefix == "" || result.Payload[0].Name == "" || result.Payload[0].AuthIndex == "" {
				t.Fatalf("unexpected wrapped provider payload: %#v", result.Payload)
			}
		})
	}
}

func TestFetchOpenAICompatibilityParsesWrappedEndpointResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementOpenAICompatibilityEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"openai-compatibility":[{"id":"custom-openai","prefix":"custom","api-key-entries":[{"api-key":"custom-key","auth-index":"custom-auth-index"}]}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchOpenAICompatibility(context.Background())
	if err != nil {
		t.Fatalf("FetchOpenAICompatibility returned error: %v", err)
	}
	if len(result.Payload) != 1 || result.Payload[0].Name != "custom-openai" || result.Payload[0].Prefix != "custom" || len(result.Payload[0].APIKeyEntries) != 1 || result.Payload[0].APIKeyEntries[0].APIKey != "custom-key" || result.Payload[0].APIKeyEntries[0].AuthIndex != "custom-auth-index" {
		t.Fatalf("unexpected wrapped openai compatibility payload: %#v", result.Payload)
	}
}

func TestFetchOpenAICompatibilityKeepsLegacyKeyAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementOpenAICompatibilityEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"openai-compatibility":[{"id":"legacy-openai","api-key-entries":[{"key":"legacy-key","auth_index":"legacy-auth-index"}]}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchOpenAICompatibility(context.Background())
	if err != nil {
		t.Fatalf("FetchOpenAICompatibility returned error: %v", err)
	}
	if len(result.Payload) != 1 || len(result.Payload[0].APIKeyEntries) != 1 || result.Payload[0].APIKeyEntries[0].APIKey != "legacy-key" || result.Payload[0].APIKeyEntries[0].AuthIndex != "legacy-auth-index" {
		t.Fatalf("unexpected legacy openai compatibility payload: %#v", result.Payload)
	}
}

func TestFetchOpenAICompatibilityUsesDedicatedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementOpenAICompatibilityEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		_, _ = w.Write([]byte(`[{"id":"custom-openai","prefix":"custom","api-keys":["custom-key"]}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second, false)
	result, err := client.FetchOpenAICompatibility(context.Background())
	if err != nil {
		t.Fatalf("FetchOpenAICompatibility returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK || len(result.Body) == 0 {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
	if len(result.Payload) != 1 || result.Payload[0].Name != "custom-openai" || result.Payload[0].Prefix != "custom" || len(result.Payload[0].APIKeyEntries) != 1 || result.Payload[0].APIKeyEntries[0].APIKey != "custom-key" {
		t.Fatalf("unexpected openai compatibility payload: %#v", result.Payload)
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
