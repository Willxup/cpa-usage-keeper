package poller_test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
)

type capturingRedisInboxWriter struct {
	messages []string
}

func (w *capturingRedisInboxWriter) Insert(_ context.Context, _ string, messages []string, _ time.Time) (int, error) {
	w.messages = messages
	return len(messages), nil
}

func TestControlAwareRedisInboxWriterDelegatesUsageOnlyBatchWithoutCopy(t *testing.T) {
	delegate := &capturingRedisInboxWriter{}
	writer := poller.NewControlAwareRedisInboxWriter(delegate, nil)
	messages := []string{`{"request_id":"one"}`, `{"request_id":"two"}`}

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceHTTPPull, messages, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected two inserted rows, got %d", inserted)
	}
	if len(delegate.messages) != len(messages) {
		t.Fatalf("expected delegated usage batch, got %+v", delegate.messages)
	}
	if &delegate.messages[0] != &messages[0] {
		t.Fatal("expected usage-only hot path to delegate the original message slice")
	}
}
