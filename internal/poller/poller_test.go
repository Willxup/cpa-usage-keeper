package poller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type syncStub struct {
	mu      sync.Mutex
	calls   int
	err     error
	started chan struct{}
	release chan struct{}
}

type syncResultStub struct {
	status string
	err    error
}

func (s *syncStub) SyncOnce(context.Context) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.release != nil {
		<-s.release
	}
	return s.err
}

func (s *syncResultStub) SyncOnce(context.Context) error {
	return s.err
}

func (s *syncResultStub) SyncStatus(context.Context) (string, error) {
	return s.status, s.err
}

func (s *syncStub) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeTicker struct {
	ch      chan time.Time
	stopped bool
}

func (t *fakeTicker) Chan() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()                  { t.stopped = true }

func TestRunExecutesImmediateAndScheduledSyncs(t *testing.T) {
	syncer := &syncStub{}
	ft := &fakeTicker{ch: make(chan time.Time, 2)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return syncer.CallCount() == 1 })
	ft.ch <- time.Now()
	waitFor(t, func() bool { return syncer.CallCount() == 2 })
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	status := p.Status()
	if status.Running {
		t.Fatal("expected poller to stop after context cancellation")
	}
	if status.LastRunAt.IsZero() {
		t.Fatal("expected LastRunAt to be set")
	}
}

func TestRunContinuesAfterSyncFailure(t *testing.T) {
	syncer := &syncStub{err: errors.New("boom")}
	ft := &fakeTicker{ch: make(chan time.Time, 1)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return syncer.CallCount() == 1 })
	ft.ch <- time.Now()
	waitFor(t, func() bool { return syncer.CallCount() == 2 })
	cancel()
	<-done

	status := p.Status()
	if status.LastError != "boom" {
		t.Fatalf("expected last error to be recorded, got %q", status.LastError)
	}
}

func TestStatusRecordsCompletedWithWarningsResult(t *testing.T) {
	syncer := &syncResultStub{
		status: "completed_with_warnings",
		err:    errors.New("fetch provider metadata: unavailable"),
	}
	p := New(syncer, time.Minute)
	p.now = func() time.Time { return time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC) }

	p.runSync(context.Background())

	status := p.Status()
	if status.LastStatus != "completed_with_warnings" {
		t.Fatalf("expected completed_with_warnings status, got %+v", status)
	}
	if status.LastError != "" || status.LastWarning != "fetch provider metadata: unavailable" {
		t.Fatalf("expected partial sync error to be recorded as warning, got %+v", status)
	}
}

func TestRunSkipsOverlappingSyncs(t *testing.T) {
	syncer := &syncStub{
		started: make(chan struct{}, 2),
		release: make(chan struct{}, 2),
	}
	ft := &fakeTicker{ch: make(chan time.Time, 2)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	select {
	case <-syncer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected initial sync to start")
	}

	ft.ch <- time.Now()
	ft.ch <- time.Now()
	time.Sleep(100 * time.Millisecond)
	if syncer.CallCount() != 1 {
		t.Fatalf("expected overlapping ticks to be skipped, got %d calls", syncer.CallCount())
	}

	syncer.release <- struct{}{}
	cancel()
	<-done

	if syncer.CallCount() != 1 {
		t.Fatalf("expected no overlapping sync runs, got %d calls", syncer.CallCount())
	}
}

func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
