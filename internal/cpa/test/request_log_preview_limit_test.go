package cpa_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestFetchRequestLogByIDAllowsSixMiBPreview(t *testing.T) {
	const sixMiB = 6 * 1024 * 1024
	body := make([]byte, sixMiB)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 10*time.Second, false)
	result, err := client.FetchRequestLogByID(context.Background(), "req-six-mib")
	if err != nil {
		t.Fatalf("FetchRequestLogByID returned error: %v", err)
	}
	if result.BodyTruncated {
		t.Fatal("expected exact six MiB response not to be truncated")
	}
	if len(result.Body) != sixMiB {
		t.Fatalf("expected six MiB response body, got %d bytes", len(result.Body))
	}
}
