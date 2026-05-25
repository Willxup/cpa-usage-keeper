package poller_test

import (
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
)

func TestRedisIngestBackoffIncreasesToMax(t *testing.T) {
	backoff := poller.NewRedisIngestBackoff(time.Second, 30*time.Second)

	got := []time.Duration{
		backoff.NextDelay(),
		backoff.NextDelay(),
		backoff.NextDelay(),
		backoff.NextDelay(),
		backoff.NextDelay(),
		backoff.NextDelay(),
		backoff.NextDelay(),
	}
	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected backoff sequence: got %v want %v", got, want)
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
