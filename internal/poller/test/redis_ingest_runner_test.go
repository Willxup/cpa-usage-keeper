package poller_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
	"github.com/sirupsen/logrus"
)

func TestRedisIngestRunnerStartupFallsBackToHTTPPull(t *testing.T) {
	writer := newFakeInboxWriter()
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		&fakePullSource{batches: [][]string{{`{"request_id":"http"}`}}},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	entry := writer.waitForInsert(t)
	cancel()
	if entry.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected HTTP source, got %q", entry.source)
	}
}

func TestRedisIngestRunnerSubscribeBackfillsBeforeReceiving(t *testing.T) {
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{{`{"request_id":"redis-backfill"}`}}},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	entry := writer.waitForInsert(t)
	cancel()
	if entry.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected Redis backfill source, got %q", entry.source)
	}
}

func TestRedisIngestRunnerSubscribeBackfillDrainsRedisBeforeReceiving(t *testing.T) {
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{
			{`{"request_id":"redis-backfill-1"}`},
			{`{"request_id":"redis-backfill-2"}`},
		}},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 1, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	first := writer.waitForInsert(t)
	second := writer.waitForInsert(t)
	cancel()
	if first.source != poller.RedisIngestSourceRedisPull || second.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected Redis backfill source for both batches, got %q and %q", first.source, second.source)
	}
}

func TestRedisIngestRunnerInfoLogsSubscribeBackfillOnce(t *testing.T) {
	logs := capturePollerLogs(t, logrus.InfoLevel)
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{{`{"request_id":"redis-backfill"}`}}},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	_ = writer.waitForInsert(t)
	output := waitForLogContains(t, logs, "redis subscribe backfill used redis pull")
	cancel()
	if strings.Contains(output, "redis ingest pulled usage messages") {
		t.Fatalf("expected per-pull loop counts to stay below info level, got logs: %s", output)
	}
}

func TestRedisIngestRunnerDebugLogsSubscribeMessageCounts(t *testing.T) {
	logs := capturePollerLogs(t, logrus.DebugLevel)
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 1, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	sub.messages <- `{"request_id":"subscribe"}`
	entry := writer.waitForInsert(t)
	output := waitForLogContains(t, logs, "redis subscribe messages received", "message_count=1", "inserted_count=1")
	cancel()
	if entry.source != poller.RedisIngestSourceSubscribe {
		t.Fatalf("expected subscribe source, got %q", entry.source)
	}
	if !strings.Contains(output, "redis subscribe messages received") || !strings.Contains(output, "message_count=1") || !strings.Contains(output, "inserted_count=1") {
		t.Fatalf("expected subscribe debug receive counts, got logs: %s", output)
	}
}

func TestRedisIngestRunnerInfoLogsHTTPRecovery(t *testing.T) {
	logs := capturePollerLogs(t, logrus.InfoLevel)
	writer := newFakeInboxWriter()
	httpSource := &fakePullSource{
		errs: []error{
			nil,
			errors.New("http failed once"),
			errors.New("http failed twice"),
			nil,
		},
		batches: [][]string{
			{`{"request_id":"http-initial"}`},
			{`{"request_id":"http-recovered"}`},
		},
	}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		httpSource,
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	initial := writer.waitForInsert(t)
	if initial.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected initial HTTP source, got %q", initial.source)
	}
	recovered := writer.waitForInsert(t)
	cancel()
	if recovered.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected recovered HTTP source, got %q", recovered.source)
	}
	output := waitForLogContains(t, logs, "redis ingest recovered", "http_pull_recovered")
	if !strings.Contains(output, "http failed once") || !strings.Contains(output, "http failed twice") {
		t.Fatalf("expected HTTP failures before recovery, got logs: %s", output)
	}
}

func TestRedisIngestRunnerSubscribeReceivingReportsSyncRunning(t *testing.T) {
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	status := waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastStatus == "subscribing"
	})
	cancel()
	if !status.SyncRunning {
		t.Fatalf("expected sync_running while subscribe is waiting, got status: %+v", status)
	}
}

func TestRedisIngestRunnerRedisPullRecoveryClearsStatusError(t *testing.T) {
	writer := newFakeInboxWriter()
	redisSource := &fakePullSource{
		errs: []error{
			nil,
			errors.New("redis failed"),
			nil,
		},
		batches: [][]string{
			{`{"request_id":"redis-initial"}`},
			{`{"request_id":"redis-recovered"}`},
		},
	}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		redisSource,
		&fakePullSource{err: errors.New("http failed")},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	initial := writer.waitForInsert(t)
	if initial.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected initial Redis source, got %q", initial.source)
	}
	// 等待第二次写入作为恢复同步点——此时 recordAvailable 已清空 LastError。
	// 不断言瞬态中间错误状态，因为 Windows 调度粒度可能导致 runner 在轮询到之前就完成恢复。
	recovered := writer.waitForInsert(t)
	if recovered.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected recovered Redis source, got %q", recovered.source)
	}
	_ = waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastError == "" && status.LastWarning == "" && status.SyncRunning
	})
	cancel()
}

func TestRedisIngestRunnerDegradedHTTPSuccessClearsStatusError(t *testing.T) {
	writer := newFakeInboxWriter()
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: failingSubscription{err: io.EOF}},
		&fakePullSource{errs: []error{nil, errors.New("redis unavailable")}},
		&fakePullSource{batches: [][]string{{`{"request_id":"http-fallback"}`}}},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	entry := writer.waitForInsert(t)
	if entry.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected degraded HTTP source, got %q", entry.source)
	}
	_ = waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastError == "" && status.LastWarning == "" && status.SyncRunning
	})
	cancel()
}

func TestRedisIngestRunnerInboxWriteFailureDoesNotConsumeFallbackSource(t *testing.T) {
	writer := newFakeInboxWriter()
	writer.err = errors.New("sqlite locked")
	httpSource := &fakePullSource{batches: [][]string{{`{"request_id":"http-should-not-consume"}`}}}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{batches: [][]string{{`{"request_id":"redis-consumed-before-write-failed"}`}}},
		httpSource,
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	attempt := writer.waitForAttempt(t)
	if attempt.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected failed write attempt from Redis source, got %q", attempt.source)
	}
	cancel()
	if calls := httpSource.callCount(); calls != 0 {
		t.Fatalf("expected writer failure not to consume HTTP fallback source, got %d calls", calls)
	}
}

func TestRedisIngestRunnerDebugLogsPullSourceAndCounts(t *testing.T) {
	logs := capturePollerLogs(t, logrus.DebugLevel)
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{{`{"request_id":"redis-backfill"}`}}},
		&fakePullSource{},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	_ = writer.waitForInsert(t)
	cancel()
	output := logs.String()
	if !strings.Contains(output, "redis ingest pulled usage messages") || !strings.Contains(output, `source="`+poller.RedisIngestSourceRedisPull+`"`) || !strings.Contains(output, "message_count=1") {
		t.Fatalf("expected debug pull source and count logs, got logs: %s", output)
	}
}

type fakeSubscribeSource struct {
	sub poller.UsageSubscription
	err error
}

func (s fakeSubscribeSource) Subscribe(context.Context) (poller.UsageSubscription, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.sub, nil
}

type blockingSubscription struct {
	messages chan string
}

func (s *blockingSubscription) Receive(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case message := <-s.messages:
		return message, nil
	}
}

func (s *blockingSubscription) Close() error { return nil }

type failingSubscription struct {
	err error
}

func (s failingSubscription) Receive(context.Context) (string, error) { return "", s.err }

func (s failingSubscription) Close() error { return nil }

type fakePullSource struct {
	mu      sync.Mutex
	batches [][]string
	errs    []error
	err     error
	calls   int
}

func (s *fakePullSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *fakePullSource) Pull(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(s.batches) == 0 {
		return nil, nil
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	return batch, nil
}

type fakeInboxInsert struct {
	source   string
	messages []string
}

type fakeInboxWriter struct {
	mu       sync.Mutex
	inserts  []fakeInboxInsert
	attempts chan fakeInboxInsert
	ch       chan fakeInboxInsert
	err      error
}

func newFakeInboxWriter() *fakeInboxWriter {
	return &fakeInboxWriter{attempts: make(chan fakeInboxInsert, 10), ch: make(chan fakeInboxInsert, 10)}
}

func (w *fakeInboxWriter) Insert(_ context.Context, source string, messages []string, _ time.Time) (int, error) {
	entry := fakeInboxInsert{source: source, messages: append([]string(nil), messages...)}
	w.attempts <- entry
	if w.err != nil {
		return 0, w.err
	}
	w.mu.Lock()
	w.inserts = append(w.inserts, entry)
	w.mu.Unlock()
	w.ch <- entry
	return len(messages), nil
}

func (w *fakeInboxWriter) waitForAttempt(t *testing.T) fakeInboxInsert {
	t.Helper()
	select {
	case entry := <-w.attempts:
		return entry
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for write attempt")
		return fakeInboxInsert{}
	}
}

func (w *fakeInboxWriter) waitForInsert(t *testing.T) fakeInboxInsert {
	t.Helper()
	select {
	case entry := <-w.ch:
		return entry
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for insert")
		return fakeInboxInsert{}
	}
}

func (w *fakeInboxWriter) lastInsert(t *testing.T) fakeInboxInsert {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.inserts) == 0 {
		t.Fatal("expected at least one insert")
	}
	return w.inserts[len(w.inserts)-1]
}

func waitForStatus(t *testing.T, runner *poller.RedisIngestRunner, match func(poller.Status) bool) poller.Status {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		status := runner.Status()
		if match(status) {
			return status
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for status, got: %+v", status)
			return status
		case <-tick.C:
		}
	}
}

func waitForLogContains(t *testing.T, logs *lockedLogBuffer, values ...string) string {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		output := logs.String()
		matched := true
		for _, value := range values {
			if !strings.Contains(output, value) {
				matched = false
				break
			}
		}
		if matched {
			return output
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for logs %v, got logs: %s", values, output)
			return output
		case <-tick.C:
		}
	}
}

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func capturePollerLogs(t *testing.T, level logrus.Level) *lockedLogBuffer {
	t.Helper()
	logs := &lockedLogBuffer{}
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(level)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return logs
}
