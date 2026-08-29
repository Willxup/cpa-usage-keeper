package poller_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/poller"
)

//go:linkname httpRawUsageMessage cpa-usage-keeper/internal/poller.httpRawUsageMessage
func httpRawUsageMessage(item []byte) string

func TestHTTPPullSourcePreservesNullPayloadForBatchCounting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/usage-queue" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Fatalf("expected count=2, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"request_id":"req-1"},null]`))
	}))
	defer server.Close()

	source := poller.NewHTTPPullSource(server.URL, "management-secret", time.Second, false, 2)
	messages, err := source.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected raw HTTP payload count to be preserved, got %d messages: %+v", len(messages), messages)
	}
	if messages[0] != `{"request_id":"req-1"}` || messages[1] != "null" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestHTTPRawUsageMessageAvoidsAllocationForIgnorablePayloads(t *testing.T) {
	nullPayload := []byte(" \n null \t")
	emptyPayload := []byte(" \r\n\t ")

	if got := httpRawUsageMessage(nullPayload); got != "null" {
		t.Fatalf("expected null payload to normalize to null, got %q", got)
	}
	if got := httpRawUsageMessage(emptyPayload); got != "" {
		t.Fatalf("expected empty payload to normalize to empty string, got %q", got)
	}

	nullAllocs := testing.AllocsPerRun(1000, func() {
		_ = httpRawUsageMessage(nullPayload)
	})
	if nullAllocs != 0 {
		t.Fatalf("expected null payload normalization to avoid allocations, got %.2f", nullAllocs)
	}

	emptyAllocs := testing.AllocsPerRun(1000, func() {
		_ = httpRawUsageMessage(emptyPayload)
	})
	if emptyAllocs != 0 {
		t.Fatalf("expected empty payload normalization to avoid allocations, got %.2f", emptyAllocs)
	}
}
