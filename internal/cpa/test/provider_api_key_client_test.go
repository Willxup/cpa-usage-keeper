package cpa_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

type providerEndpointResult struct {
	statusCode    int
	body          []byte
	keys          []providerconfig.ProviderKeyConfig
	compatibility []providerconfig.OpenAICompatibilityConfig
}

type providerEndpointCase struct {
	name        string
	path        string
	directBody  string
	wrappedBody string
	wantOpenAI  bool
	wantMeta    bool
}

func TestProviderAPIKeyClientsUseDedicatedEndpointsAndDecodePayloads(t *testing.T) {
	cases := []providerEndpointCase{
		{name: "codex", path: "/v0/management/codex-api-key", directBody: `[{"api-key":"codex-key","prefix":"codex-prefix","base-url":"https://codex.example/v1","name":"Codex","auth-index":"codex-auth"}]`, wrappedBody: `{"codex-api-key":[{"api-key":"codex-key","prefix":"codex-prefix","base-url":"https://codex.example/v1","name":"Codex","auth-index":"codex-auth"}]}`},
		{name: "xai", path: "/v0/management/xai-api-key", directBody: `[{"api-key":"xai-key","prefix":"xai-prefix","base-url":"https://api.x.ai/v1","name":"xAI","websockets":true,"auth-index":"xai-auth"}]`, wrappedBody: `{"xai-api-key":[{"api-key":"xai-key","prefix":"xai-prefix","base-url":"https://api.x.ai/v1","name":"xAI","websockets":true,"auth-index":"xai-auth"}]}`},
		{name: "gemini", path: "/v0/management/gemini-api-key", directBody: `[{"api-key":"gemini-key","prefix":"gemini-prefix","base-url":"https://gemini.example/v1","name":"Gemini","auth-index":"gemini-auth"}]`, wrappedBody: `{"gemini-api-key":[{"api-key":"gemini-key","prefix":"gemini-prefix","base-url":"https://gemini.example/v1","name":"Gemini","auth-index":"gemini-auth"}]}`},
		{name: "gemini-interactions", path: "/v0/management/interactions-api-key", directBody: `[{"api-key":"interactions-key","prefix":"interactions-prefix","base-url":"https://interactions.example/v1","name":"Interactions","auth-index":"interactions-auth"}]`, wrappedBody: `{"interactions-api-key":[{"api-key":"interactions-key","prefix":"interactions-prefix","base-url":"https://interactions.example/v1","name":"Interactions","auth-index":"interactions-auth"}]}`},
		{name: "claude", path: "/v0/management/claude-api-key", directBody: `[{"api-key":"claude-key","prefix":"claude-prefix","base-url":"https://claude.example/v1","name":"Claude","auth-index":"claude-auth"}]`, wrappedBody: `{"claude-api-key":[{"api-key":"claude-key","prefix":"claude-prefix","base-url":"https://claude.example/v1","name":"Claude","auth-index":"claude-auth"}]}`},
		{name: "vertex", path: "/v0/management/vertex-api-key", directBody: `[{"api-key":"vertex-key","prefix":"vertex-prefix","base-url":"https://vertex.example/v1","name":"Vertex","auth-index":"vertex-auth"}]`, wrappedBody: `{"vertex-api-key":[{"api-key":"vertex-key","prefix":"vertex-prefix","base-url":"https://vertex.example/v1","name":"Vertex","auth-index":"vertex-auth"}]}`},
		{name: "meta", path: "/v0/management/meta-api-key", directBody: `[{"apiKey":"meta-key","prefix":"meta-prefix","base_url":"https://meta.example/v1","name":"Meta Team","authIndex":"meta-auth","priority":3,"disabled":false,"note":"meta note"}]`, wrappedBody: `{"meta-api-key":[{"api-key":"meta-key","prefix":"meta-prefix","base-url":"https://meta.example/v1","name":"Meta Team","auth-index":"meta-auth","priority":3,"disabled":false,"note":"meta note"}]}`, wantMeta: true},
		{name: "openai", path: "/v0/management/openai-compatibility", directBody: `[{"name":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","api-key-entries":[{"api-key":"openai-key","auth-index":"openai-auth"}]}]`, wrappedBody: `{"openai-compatibility":[{"id":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","api-key-entries":[{"key":"openai-key","auth_index":"openai-auth"}]}]}`, wantOpenAI: true},
	}
	for _, tc := range cases {
		for _, variant := range []struct {
			name string
			body string
		}{
			{name: "direct", body: tc.directBody},
			{name: "wrapped", body: tc.wrappedBody},
		} {
			t.Run(tc.name+"/"+variant.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("method = %q, want GET", r.Method)
					}
					if r.URL.Path != tc.path {
						t.Errorf("path = %q, want %q", r.URL.Path, tc.path)
					}
					if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
						t.Errorf("Authorization = %q", got)
					}
					_, _ = w.Write([]byte(variant.body))
				}))
				defer server.Close()
				client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
				result, err := fetchProviderEndpoint(context.Background(), client, tc.name)
				if err != nil {
					t.Fatalf("fetch endpoint: %v", err)
				}
				if result.statusCode != http.StatusOK || len(result.body) == 0 {
					t.Fatalf("result metadata = status:%d body:%q", result.statusCode, string(result.body))
				}
				if tc.wantOpenAI {
					if len(result.compatibility) != 1 || result.compatibility[0].Name != "OpenRouter" || result.compatibility[0].Prefix != "openrouter" || result.compatibility[0].BaseURL != "https://openrouter.ai/api/v1" || len(result.compatibility[0].APIKeyEntries) != 1 || result.compatibility[0].APIKeyEntries[0].APIKey != "openai-key" || result.compatibility[0].APIKeyEntries[0].AuthIndex != "openai-auth" {
						t.Fatalf("openai payload = %#v", result.compatibility)
					}
					return
				}
				if len(result.keys) != 1 || result.keys[0].APIKey == "" || result.keys[0].Prefix == "" || result.keys[0].BaseURL == "" || result.keys[0].Name == "" || result.keys[0].AuthIndex == "" {
					t.Fatalf("provider payload = %#v", result.keys)
				}
				if tc.wantMeta {
					entry := result.keys[0]
					if entry.APIKey != "meta-key" || entry.BaseURL != "https://meta.example/v1" || entry.AuthIndex != "meta-auth" || entry.Priority == nil || *entry.Priority != 3 || entry.Disabled == nil || *entry.Disabled || entry.Note == nil || *entry.Note != "meta note" {
						t.Fatalf("meta payload = %#v", entry)
					}
				}
			})
		}
	}
}

func TestProviderAPIKeyClientClassifiesEmptyAndBlankBodies(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "direct-array", body: `[]`},
		{name: "direct-null", body: `null`},
		{name: "wrapped-array", body: `{"xai-api-key":[]}`},
		{name: "blank-body", body: "   \n", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := client.FetchXAIAPIKeys(context.Background())
			if tc.wantError {
				if err == nil || result == nil {
					t.Fatalf("blank body result=%#v err=%v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("empty payload: %v", err)
			}
			if result.StatusCode != http.StatusOK || len(result.Payload) != 0 {
				t.Fatalf("empty result = %#v", result)
			}
		})
	}
}

func TestOptionalProviderEndpointsReturnTyped404WithoutLeakingBody(t *testing.T) {
	for _, source := range []string{"gemini-interactions", "xai", "meta"} {
		t.Run(source, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"secret":"body-must-not-leak"}`))
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := fetchProviderEndpoint(context.Background(), client, source)
			if err == nil || result.statusCode != http.StatusNotFound {
				t.Fatalf("typed 404 result=%#v err=%v", result, err)
			}
			if strings.Contains(err.Error(), "body-must-not-leak") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

func fetchProviderEndpoint(ctx context.Context, client *cpa.Client, source string) (providerEndpointResult, error) {
	switch source {
	case "codex":
		result, err := client.FetchCodexAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "xai":
		result, err := client.FetchXAIAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "gemini":
		result, err := client.FetchGeminiAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "gemini-interactions":
		result, err := client.FetchInteractionsAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "claude":
		result, err := client.FetchClaudeAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "vertex":
		result, err := client.FetchVertexAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "meta":
		result, err := client.FetchMetaAPIKeys(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "openai":
		result, err := client.FetchOpenAICompatibility(ctx)
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, compatibility: result.Payload}, err
	default:
		return providerEndpointResult{}, fmt.Errorf("unknown provider source: %s", source)
	}
}
