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
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Errorf("expected count=2, got %q", got)
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
	for _, tc := range []struct {
		body []byte
		want string
	}{
		{[]byte(" \n null \t"), "null"},
		{[]byte(" \r\n\t "), ""},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := httpRawUsageMessage(tc.body); got != tc.want {
				t.Fatalf("normalized payload = %q, want %q", got, tc.want)
			}
			allocs := testing.AllocsPerRun(1000, func() { _ = httpRawUsageMessage(tc.body) })
			if allocs != 0 {
				t.Fatalf("expected normalization without allocations, got %.2f", allocs)
			}
		})
	}
}
