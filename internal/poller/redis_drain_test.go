package poller

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/sirupsen/logrus"
)

type redisDrainSyncStub struct {
	mu            sync.Mutex
	results       []*service.RedisBatchSyncResult
	errs          []error
	metadataFlags []bool
	calls         int
	legacyCalls   int
	legacyErr     error
	metadataCalls int
	metadataErr   error
	started       chan struct{}
	release       chan struct{}
}

func (s *redisDrainSyncStub) SyncRedisBatch(ctx context.Context, syncMetadata bool) (*service.RedisBatchSyncResult, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.metadataFlags = append(s.metadataFlags, syncMetadata)
	var result *service.RedisBatchSyncResult
	if len(s.results) >= call {
		result = s.results[call-1]
	} else if len(s.results) > 0 {
		result = s.results[len(s.results)-1]
	} else {
		result = &service.RedisBatchSyncResult{Status: "completed", InsertedEvents: 1}
	}
	var err error
	if len(s.errs) >= call {
		err = s.errs[call-1]
	} else if len(s.errs) > 0 {
		err = s.errs[len(s.errs)-1]
	}
	s.mu.Unlock()

	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, err
}

func (s *redisDrainSyncStub) SyncLegacyStatus(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.legacyCalls++
	if s.legacyErr != nil {
		return "", s.legacyErr
	}
	return "completed", nil
}

func (s *redisDrainSyncStub) SyncMetadata(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataCalls++
	return s.metadataErr
}

func (s *redisDrainSyncStub) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *redisDrainSyncStub) MetadataFlags() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.metadataFlags...)
}

func (s *redisDrainSyncStub) MetadataCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metadataCalls
}

func TestRedisDrainRunsNextBatchImmediatelyAfterNonEmptyResult(t *testing.T) {
	syncer := &redisDrainSyncStub{results: []*service.RedisBatchSyncResult{{Status: "completed", InsertedEvents: 1}}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	drain.sleep = func(context.Context, time.Duration) bool {
		t.Fatal("did not expect sleep between non-empty batches")
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() >= 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRedisDrainSleepsAfterEmptyBatch(t *testing.T) {
	syncer := &redisDrainSyncStub{results: []*service.RedisBatchSyncResult{{Empty: true, Status: "empty"}}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: 15 * time.Millisecond, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	slept := make(chan time.Duration, 1)
	drain.sleep = func(ctx context.Context, d time.Duration) bool {
		slept <- d
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case d := <-slept:
		if d != 15*time.Millisecond {
			t.Fatalf("expected idle sleep, got %s", d)
		}
	case <-time.After(time.Second):
		t.Fatal("expected empty batch to sleep")
	}
	if syncer.MetadataCallCount() != 1 {
		t.Fatalf("expected due metadata sync on empty batch, got %d", syncer.MetadataCallCount())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRedisDrainBacksOffAfterTransientError(t *testing.T) {
	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{{Status: "failed"}},
		errs:    []error{errors.New("dial failed")},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: 25 * time.Millisecond, MetadataInterval: time.Hour})
	slept := make(chan time.Duration, 1)
	drain.sleep = func(ctx context.Context, d time.Duration) bool {
		slept <- d
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case d := <-slept:
		if d != 25*time.Millisecond {
			t.Fatalf("expected error backoff sleep, got %s", d)
		}
	case <-time.After(time.Second):
		t.Fatal("expected error backoff sleep")
	}
	status := drain.Status()
	if status.LastError != "dial failed" || !status.Running {
		t.Fatalf("expected running drain with recorded error, got %+v", status)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRedisDrainLogsHardBatchFailureDuringRun(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{{Status: "failed"}},
		errs:    []error{errors.New("dial failed")},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{
		IdleInterval:         time.Hour,
		ErrorBackoff:         25 * time.Millisecond,
		MetadataInterval:     time.Hour,
		EnableLegacyFallback: true,
	})
	slept := make(chan time.Duration, 1)
	drain.sleep = func(ctx context.Context, d time.Duration) bool {
		slept <- d
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case <-slept:
	case <-time.After(time.Second):
		t.Fatal("expected error backoff sleep")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := output.String()
	for _, want := range []string{
		"level=error",
		"msg=\"redis drain batch failed\"",
		"error=\"dial failed\"",
		"status=failed",
		"sync_metadata=true",
		"legacy_fallback_enabled=true",
		"auth_error=false",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, content)
		}
	}
}

func TestRedisDrainLogsHardBatchFailureWithNilResult(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{nil},
		errs:    []error{errors.New("redis read failed")},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{
		IdleInterval:     time.Hour,
		ErrorBackoff:     25 * time.Millisecond,
		MetadataInterval: time.Hour,
	})
	slept := make(chan time.Duration, 1)
	drain.sleep = func(ctx context.Context, d time.Duration) bool {
		slept <- d
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case <-slept:
	case <-time.After(time.Second):
		t.Fatal("expected error backoff sleep")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := output.String()
	for _, want := range []string{
		"level=error",
		"msg=\"redis drain batch failed\"",
		"error=\"redis read failed\"",
		"status=",
		"empty=false",
		"inserted_events=0",
		"deduped_events=0",
		"sync_metadata=true",
		"legacy_fallback_enabled=false",
		"auth_error=false",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, content)
		}
	}
}

func TestRedisDrainContinuesImmediatelyAfterMetadataWarning(t *testing.T) {
	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{{Status: "completed_with_warnings", InsertedEvents: 1}},
		errs:    []error{errors.New("fetch provider metadata: unavailable")},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	drain.sleep = func(context.Context, time.Duration) bool {
		t.Fatal("did not expect metadata warning to trigger backoff")
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() >= 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if status := drain.Status(); status.LastWarning == "" || status.LastError != "" {
		t.Fatalf("expected metadata warning without hard error, got %+v", status)
	}
}

func TestRedisDrainDoesNotErrorLogContextCancellation(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{nil},
		errs:    []error{context.Canceled},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: 25 * time.Millisecond, MetadataInterval: time.Hour})
	drain.sleep = func(context.Context, time.Duration) bool {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() >= 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(output.String(), "level=error") {
		t.Fatalf("did not expect error log for context cancellation, got %q", output.String())
	}
}

func TestRedisDrainDoesNotErrorLogAlreadyRunning(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: 25 * time.Millisecond, MetadataInterval: time.Hour})
	drain.syncRunning = true
	drain.sleep = func(context.Context, time.Duration) bool {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Run to stop after already-running backoff hook")
	}
	if strings.Contains(output.String(), "level=error") {
		t.Fatalf("did not expect error log for already-running sync, got %q", output.String())
	}
}

func TestRedisDrainDoesNotErrorLogCompletedWithWarnings(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results: []*service.RedisBatchSyncResult{{Status: "completed_with_warnings", InsertedEvents: 1}},
		errs:    []error{errors.New("metadata unavailable")},
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	drain.sleep = func(context.Context, time.Duration) bool {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() >= 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(output.String(), "level=error") {
		t.Fatalf("did not expect error log for warning result, got %q", output.String())
	}
}

func TestRedisDrainDoesNotErrorLogLegacyFallbackCancellation(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results:   []*service.RedisBatchSyncResult{{Status: "failed"}},
		errs:      []error{errors.New("redis unavailable")},
		legacyErr: context.Canceled,
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{
		IdleInterval:           time.Hour,
		ErrorBackoff:           25 * time.Millisecond,
		MetadataInterval:       time.Hour,
		EnableLegacyFallback:   true,
		LegacyFallbackInterval: time.Second,
	})
	drain.sleep = func(context.Context, time.Duration) bool {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Run to stop after fallback cancellation")
	}

	content := output.String()
	if strings.Contains(content, "redis drain legacy fallback failed") {
		t.Fatalf("did not expect fallback error log for context cancellation, got %q", content)
	}
}

func TestRedisDrainLogsLegacyFallbackFailure(t *testing.T) {
	var output bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	}()

	syncer := &redisDrainSyncStub{
		results:   []*service.RedisBatchSyncResult{{Status: "failed"}},
		errs:      []error{errors.New("redis unavailable")},
		legacyErr: errors.New("legacy pull failed"),
	}
	drain := NewRedisDrain(syncer, RedisDrainConfig{
		IdleInterval:           time.Hour,
		ErrorBackoff:           25 * time.Millisecond,
		MetadataInterval:       time.Hour,
		EnableLegacyFallback:   true,
		LegacyFallbackInterval: time.Second,
	})
	drain.sleep = func(ctx context.Context, d time.Duration) bool {
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool {
		return strings.Contains(drain.Status().LastWarning, "legacy fallback: legacy pull failed")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := output.String()
	for _, want := range []string{
		"level=error",
		"msg=\"redis drain legacy fallback failed\"",
		"error=\"legacy pull failed\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, content)
		}
	}
}

func TestRedisDrainMetadataFlagUsesInterval(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	syncer := &redisDrainSyncStub{results: []*service.RedisBatchSyncResult{{Status: "completed", InsertedEvents: 1}}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: 30 * time.Second})
	drain.now = func() time.Time { return now }
	drain.sleep = func(context.Context, time.Duration) bool { return false }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- drain.Run(ctx) }()
	waitFor(t, func() bool { return syncer.CallCount() >= 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	flags := syncer.MetadataFlags()
	if len(flags) < 2 || !flags[0] || flags[1] {
		t.Fatalf("expected first metadata sync only before interval elapses, got %v", flags)
	}
}
