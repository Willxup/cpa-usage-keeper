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

func TestFetchModelsFetchesAPIKeyAndParsesOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/config":
			if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
				t.Fatalf("expected management Authorization header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"api-keys":["", "   ", "normal-api-key"]}`))
		case "/v1/models":
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

func TestFetchModelsRejectsMissingAPIKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/config" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"api-keys":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing API keys error")
	}
}

func TestFetchModelsIgnoresProviderKeysWhenCPAAPIKeysAreMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/config":
			_, _ = w.Write([]byte(`{
				"claude-api-key":[{"api-key":"provider-claude-key"}],
				"codex-api-key":[{"api-key":"provider-codex-key"}],
				"openai-compatibility":[{"api-keys":["provider-openai-key"]}]
			}`))
		case "/v1/models":
			t.Fatal("FetchModels should not request models with provider keys")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "management-secret", 2*time.Second)
	if _, err := client.FetchModels(context.Background()); err == nil {
		t.Fatal("expected missing CPA API keys error")
	}
}

func TestFetchModelsHandlesModelNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/config":
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case "/v1/models":
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
		case "/v0/management/config":
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case "/v1/models":
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
		case "/v0/management/config":
			_, _ = w.Write([]byte(`{"api-keys":["normal-api-key"]}`))
		case "/v1/models":
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
