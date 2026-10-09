package poller_test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
)

func TestRedisIngestBackoffIncreasesToMax(t *testing.T) {
	backoff := poller.NewRedisIngestBackoff(time.Second, 30*time.Second)

	for index, want := range []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 30 * time.Second, 30 * time.Second,
	} {
		if got := backoff.NextDelay(); got != want {
			t.Fatalf("delay %d = %s, want %s", index, got, want)
		}
	}
}

func TestRedisIngestBackoffReset(t *testing.T) {
	backoff := poller.NewRedisIngestBackoff(time.Second, 30*time.Second)
	_ = backoff.NextDelay()
	_ = backoff.NextDelay()

	backoff.Reset()

	if got := backoff.NextDelay(); got != time.Second {
		t.Fatalf("expected reset delay 1s, got %s", got)
	}
}
