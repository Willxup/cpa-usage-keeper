package poller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
	"github.com/sirupsen/logrus"
)

func TestRedisIngestStartupAllFailedBackoffIncreasesFromTenSeconds(t *testing.T) {
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		&fakePullSource{err: errors.New("http unavailable")},
		newFakeInboxWriter(),
		poller.RedisIngestRunnerConfig{IdleInterval: time.Millisecond, BatchSize: 10},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisIngestRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		if len(delays) == 3 {
			cancel()
			return false
		}
		return true
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	requireDurations(t, delays, []time.Duration{10 * time.Second, 20 * time.Second, 30 * time.Second})
}

func TestRedisIngestRunnerHTTPPullFailureKeepsOneSecondInitialBackoff(t *testing.T) {
	httpSource := &fakePullSource{errs: []error{nil, errors.New("http unavailable")}}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		httpSource,
		newFakeInboxWriter(),
		poller.RedisIngestRunnerConfig{IdleInterval: time.Hour, BatchSize: 10},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisIngestRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if calls := httpSource.callCount(); calls != 2 {
		t.Fatalf("expected startup HTTP success followed by one failed HTTP retry, got %d calls", calls)
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}

func TestRedisIngestRunnerStartupHTTPInboxWriteFailureUsesOneSecondBackoff(t *testing.T) {
	logs := capturePollerLogs(t, logrus.DebugLevel)
	writer := newFakeInboxWriter()
	writer.err = errors.New("sqlite locked")
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		&fakePullSource{batches: [][]string{{`{"request_id":"http"}`}}},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: time.Millisecond, BatchSize: 10},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var delays []time.Duration
	setRedisIngestRunnerSleep(t, runner, func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		cancel()
		return false
	})
	done := make(chan struct{})
	go func() {
		_ = runner.Run(ctx)
		close(done)
	}()

	attempt := writer.waitForAttempt(t)
	if attempt.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected failed write attempt from HTTP source, got %q", attempt.source)
	}
	output := waitForLogContains(t, logs, "redis ingest inbox write retry scheduled", "retry_after=1s")
	if output == "" {
		t.Fatal("expected inbox write retry log")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner to stop")
	}
	requireDurations(t, delays, []time.Duration{time.Second})
}
