package poller

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
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

func (s *syncStub) SyncOnce(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
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

func TestRunLogsPollerStart(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	syncer := &syncStub{}
	ft := &fakeTicker{ch: make(chan time.Time)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("poller returned error: %v", err)
	}

	content := logs.String()
	if !strings.Contains(content, "level=info") || !strings.Contains(content, "msg=\"legacy export poller task started\"") {
		t.Fatalf("expected poller start info log, got %q", content)
	}
}

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

func TestPollerRunLogsSyncFailure(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	syncer := &syncStub{err: errors.New("boom")}
	ft := &fakeTicker{ch: make(chan time.Time, 1)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return syncer.CallCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := logs.String()
	for _, want := range []string{
		"level=error",
		"msg=\"poller sync failed\"",
		"error=boom",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, content)
		}
	}
}

func TestPollerRunLogsFailedStatusSyncFailure(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	syncer := &syncResultStub{
		status: "failed",
		err:    errors.New("fetch usage export: unavailable"),
	}
	ft := &fakeTicker{ch: make(chan time.Time, 1)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return p.Status().LastError == "fetch usage export: unavailable" })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := logs.String()
	for _, want := range []string{
		"level=error",
		"msg=\"poller sync failed\"",
		"error=\"fetch usage export: unavailable\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, content)
		}
	}
}

func TestPollerRunDoesNotErrorLogContextCancellation(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	syncer := &syncStub{err: context.Canceled}
	ft := &fakeTicker{ch: make(chan time.Time, 1)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return syncer.CallCount() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(logs.String(), "level=error") {
		t.Fatalf("did not expect error log for context cancellation, got %q", logs.String())
	}
}

func TestPollerRunDoesNotErrorLogAlreadyRunning(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	p := New(&syncStub{}, time.Minute)
	p.syncRunning = true

	p.runBackgroundSync(context.Background())

	if strings.Contains(logs.String(), "level=error") {
		t.Fatalf("did not expect error log for already-running sync, got %q", logs.String())
	}
}

func TestPollerRunDoesNotErrorLogCompletedWithWarnings(t *testing.T) {
	logs := capturePollerLogrusOutput(t)
	syncer := &syncResultStub{
		status: "completed_with_warnings",
		err:    errors.New("metadata unavailable"),
	}
	ft := &fakeTicker{ch: make(chan time.Time, 1)}
	p := New(syncer, time.Minute)
	p.ticker = func(time.Duration) ticker { return ft }
	p.now = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	waitFor(t, func() bool { return p.Status().LastWarning == "metadata unavailable" })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(logs.String(), "level=error") {
		t.Fatalf("did not expect error log for warning result, got %q", logs.String())
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

func TestSyncNowSkipsOverlappingSyncs(t *testing.T) {
	syncer := &syncStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}, 1),
	}
	p := New(syncer, time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	firstSyncDone := make(chan error, 1)
	go func() { firstSyncDone <- p.SyncNow(ctx) }()

	select {
	case <-syncer.started:
	case <-time.After(time.Second):
		t.Fatal("expected initial sync to start")
	}

	if err := p.SyncNow(ctx); !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatalf("expected overlapping sync to be skipped, got %v", err)
	}
	if syncer.CallCount() != 1 {
		t.Fatalf("expected no overlapping sync runs, got %d calls", syncer.CallCount())
	}

	syncer.release <- struct{}{}
	select {
	case err := <-firstSyncDone:
		if err != nil {
			t.Fatalf("initial sync returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected initial sync to finish")
	}
}

func capturePollerLogrusOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	var logs bytes.Buffer
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
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
