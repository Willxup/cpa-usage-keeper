package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type metadataSyncStub struct {
	mu     sync.Mutex
	calls  int
	errs   []error
	onCall func(int)
}

func (s *metadataSyncStub) SyncMetadata(context.Context) error {
	var err error

	s.mu.Lock()
	s.calls++
	call := s.calls
	if len(s.errs) >= call {
		err = s.errs[call-1]
	} else if len(s.errs) > 0 {
		err = s.errs[len(s.errs)-1]
	}
	onCall := s.onCall
	s.mu.Unlock()

	if onCall != nil {
		onCall(call)
	}
	return err
}

func (s *metadataSyncStub) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type metadataSyncErrContext struct {
	context.Context

	mu   sync.Mutex
	err  error
	done chan struct{}
}

func newMetadataSyncErrContext() *metadataSyncErrContext {
	return &metadataSyncErrContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *metadataSyncErrContext) Done() <-chan struct{} {
	return c.done
}

func (c *metadataSyncErrContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *metadataSyncErrContext) SetErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

func (c *metadataSyncErrContext) CloseDone() {
	close(c.done)
}

func TestMetadataSyncRunnerRunsImmediatelyThenAtInterval(t *testing.T) {
	syncer := &metadataSyncStub{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call >= 2 {
			cancel()
		}
	}
	runner := NewMetadataSyncRunner(syncer, time.Millisecond)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 2 {
		t.Fatalf("expected two metadata sync calls, got %d", got)
	}
}

func TestMetadataSyncRunnerLogsFailureAndContinues(t *testing.T) {
	logs := captureAppInfoLogs(t)
	syncer := &metadataSyncStub{errs: []error{errors.New("metadata endpoint failed"), nil}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call >= 2 {
			cancel()
		}
	}
	runner := NewMetadataSyncRunner(syncer, time.Millisecond)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 2 {
		t.Fatalf("expected runner to continue after metadata error, got %d calls", got)
	}
	content := logs.String()
	if !strings.Contains(content, "level=error") || !strings.Contains(content, "msg=\"metadata sync failed\"") {
		t.Fatalf("expected metadata sync failure error log, got %q", content)
	}
}

func TestMetadataSyncRunnerValidatesConfig(t *testing.T) {
	if err := NewMetadataSyncRunner(nil, time.Minute).Run(context.Background()); err == nil {
		t.Fatal("expected nil syncer validation error")
	}
	if err := NewMetadataSyncRunner(&metadataSyncStub{}, 0).Run(context.Background()); err == nil {
		t.Fatal("expected non-positive interval validation error")
	}
}

func TestMetadataSyncRunnerDefaultsRefreshDebounceToThreeSeconds(t *testing.T) {
	runner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Minute)

	if runner.refreshDebounce != 3*time.Second {
		t.Fatalf("expected default refresh debounce to be 3s, got %s", runner.refreshDebounce)
	}
}

func TestMetadataSyncRunnerRefreshSupportMakesPeriodicTickNoop(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Millisecond)
	runner.MarkRefreshSupported()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call == 1 {
			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()
		}
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 1 {
		t.Fatalf("expected only startup sync in notification mode, got %d", got)
	}
}

func TestMetadataSyncRunnerLogsModeSwitches(t *testing.T) {
	logs := captureAppInfoLogs(t)
	runner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Minute)

	runner.MarkRefreshSupported()
	runner.MarkRefreshSupported()
	runner.MarkRefreshPollingRequired("redis_degraded_http")
	runner.MarkRefreshPollingRequired("redis_degraded_http")
	content := logs.String()
	if strings.Count(content, "metadata sync switched to notification mode") != 1 {
		t.Fatalf("expected one log per real mode switch, got:\n%s", content)
	}
	if !strings.Contains(content, "level=info") || !strings.Contains(content, "source=support_refresh") {
		t.Fatalf("expected notification mode log with source, got:\n%s", content)
	}
	if strings.Count(content, "metadata sync switched to polling mode") != 1 {
		t.Fatalf("expected one polling mode restore log, got:\n%s", content)
	}
	if strings.Count(content, "level=info") < 2 || !strings.Contains(content, "reason=redis_degraded_http") {
		t.Fatalf("expected polling mode restore info log with reason, got:\n%s", content)
	}
}

func TestMetadataSyncRunnerRefreshSupportDoesNotSyncWithoutRefreshRequest(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Millisecond)
	runner.refreshDebounce = time.Millisecond
	runner.MarkRefreshSupported()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call == 1 {
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
		}
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 1 {
		t.Fatalf("expected support-only notification mode not to sync without refresh, got %d", got)
	}
}

func TestMetadataSyncRunnerRefreshRequestDebounces(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Hour)
	runner.refreshDebounce = 5 * time.Millisecond
	runner.RequestMetadataRefresh()
	runner.RequestMetadataRefresh()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call >= 2 {
			cancel()
		}
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 2 {
		t.Fatalf("expected startup sync plus one debounced refresh sync, got %d", got)
	}
}

func TestMetadataSyncRunnerRefreshRequestUsesTrailingDebounce(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Hour)
	runner.refreshDebounce = 150 * time.Millisecond
	calls := make(chan time.Time, 3)
	syncer.onCall = func(int) {
		calls <- time.Now()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for startup sync")
	}

	runner.RequestMetadataRefresh()
	time.Sleep(20 * time.Millisecond)
	secondRefreshAt := time.Now()
	runner.RequestMetadataRefresh()

	var refreshedAt time.Time
	select {
	case refreshedAt = <-calls:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debounced refresh")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner to stop")
	}
	if elapsed := refreshedAt.Sub(secondRefreshAt); elapsed < 120*time.Millisecond {
		t.Fatalf("expected debounce to wait after the last refresh request, elapsed %s", elapsed)
	}
	if got := syncer.CallCount(); got != 2 {
		t.Fatalf("expected startup sync plus one trailing refresh sync, got %d", got)
	}
}

func TestMetadataSyncRunnerSkipsDebouncedRefreshAfterContextError(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Hour)
	runner.refreshDebounce = time.Millisecond
	ctx := newMetadataSyncErrContext()
	syncer.onCall = func(call int) {
		if call == 1 {
			ctx.SetErr(context.Canceled)
			runner.RequestMetadataRefresh()
			return
		}
		ctx.CloseDone()
	}

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner to stop")
	}
	if got := syncer.CallCount(); got != 1 {
		t.Fatalf("expected debounce to skip sync after context error, got %d calls", got)
	}
}

func TestMetadataSyncRunnerFallbackRestoresPolling(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, time.Millisecond)
	runner.MarkRefreshSupported()
	runner.MarkRefreshPollingRequired("redis_degraded_http")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.onCall = func(call int) {
		if call >= 2 {
			cancel()
		}
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := syncer.CallCount(); got != 2 {
		t.Fatalf("expected startup sync plus restored periodic sync, got %d", got)
	}
}
