package cpa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchUsageExportSendsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("expected Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":1,"exported_at":"2026-04-16T00:00:00Z","usage":{}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", 2*time.Second)
	result, err := client.FetchUsageExport(context.Background())
	if err != nil {
		t.Fatalf("FetchUsageExport returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
}

func TestFetchUsageExportHandlesUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid management key"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", 2*time.Second)
	_, err := client.FetchUsageExport(context.Background())
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
}

func TestFetchUsageExportRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", 2*time.Second)
	_, err := client.FetchUsageExport(context.Background())
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestFetchExternalAPIKeysSendsBearerTokenAndParsesExternalKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementExternalAPIKeysEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api-keys":["", "   ", "normal-api-key"]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	result, err := client.FetchExternalAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("FetchExternalAPIKeys returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}
	if string(result.Body) != `{"api-keys":["", "   ", "normal-api-key"]}` {
		t.Fatalf("unexpected body: %s", string(result.Body))
	}
	if len(result.Payload.ExternalAPIKeys) != 3 || result.Payload.ExternalAPIKeys[2] != "normal-api-key" {
		t.Fatalf("unexpected external API keys payload: %#v", result.Payload)
	}
}

func TestFetchModelsUsesExternalAPIKeyAndParsesOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementExternalAPIKeysEndpoint:
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

	client := NewClient(server.URL, "management-secret", 2*time.Second)
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

func TestFetchModelsRejectsMissingExternalAPIKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementExternalAPIKeysEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"api-keys":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing external API keys error")
	}
}

func TestFetchModelsDoesNotUseProviderEndpointsWhenCPAExternalAPIKeysAreMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementExternalAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":[]}`))
		case cpaManagementClaudeAPIKeyEndpoint, cpaManagementCodexAPIKeyEndpoint, cpaManagementOpenAICompatibilityEndpoint, cpaModelsEndpoint:
			t.Fatalf("FetchModels should not request %s when CPA external API keys are missing", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing CPA external API keys error")
	}
}

func TestFetchModelsHandlesModelNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementExternalAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			http.Error(w, `{"error":"unavailable"}`, http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected non-success status error")
	}
}

func TestFetchModelsRejectsRedirectStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementExternalAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			w.WriteHeader(http.StatusFound)
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected redirect status error")
	}
}

func TestFetchModelsRejectsInvalidModelsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cpaManagementExternalAPIKeysEndpoint:
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case cpaModelsEndpoint:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not-json`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	_, err := client.FetchModels(context.Background())
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}

func TestProviderMetadataFetchersUseDedicatedEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		fetch    func(context.Context, *Client) (*ProviderKeyConfigResult, error)
		response string
	}{
		{
			name: "gemini",
			path: cpaManagementGeminiAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*ProviderKeyConfigResult, error) {
				return client.FetchGeminiAPIKeys(ctx)
			},
			response: `[{"apiKey":"gemini-key","prefix":"gemini-prefix","name":"Gemini"}]`,
		},
		{
			name: "claude",
			path: cpaManagementClaudeAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*ProviderKeyConfigResult, error) {
				return client.FetchClaudeAPIKeys(ctx)
			},
			response: `[{"api-key":"claude-key","prefix":"claude-prefix","name":"Claude"}]`,
		},
		{
			name: "codex",
			path: cpaManagementCodexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*ProviderKeyConfigResult, error) {
				return client.FetchCodexAPIKeys(ctx)
			},
			response: `[{"key":"codex-key","prefix":"codex-prefix","name":"Codex"}]`,
		},
		{
			name: "vertex",
			path: cpaManagementVertexAPIKeyEndpoint,
			fetch: func(ctx context.Context, client *Client) (*ProviderKeyConfigResult, error) {
				return client.FetchVertexAPIKeys(ctx)
			},
			response: `[{"apiKey":"vertex-key","prefix":"vertex-prefix","name":"Vertex"}]`,
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

			client := NewClient(server.URL, "management-secret", 2*time.Second)
			result, err := tt.fetch(context.Background(), client)
			if err != nil {
				t.Fatalf("fetch returned error: %v", err)
			}
			if result.StatusCode != http.StatusOK || len(result.Body) == 0 {
				t.Fatalf("unexpected result metadata: %+v", result)
			}
			if len(result.Payload) != 1 || result.Payload[0].APIKey == "" || result.Payload[0].Prefix == "" || result.Payload[0].Name == "" {
				t.Fatalf("unexpected provider payload: %#v", result.Payload)
			}
		})
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

	client := NewClient(server.URL, "management-secret", 2*time.Second)
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
