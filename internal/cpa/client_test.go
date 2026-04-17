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
