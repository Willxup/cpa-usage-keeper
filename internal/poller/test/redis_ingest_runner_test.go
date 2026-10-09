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

func TestRedisIngestRunnerNotifiesMetadataOnInitialConnectionOnce(t *testing.T) {
	observer := &controlObserverStub{}
	writer := newFakeInboxWriter()
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		&fakePullSource{batches: [][]string{{`{"request_id":"http"}`}, {`{"request_id":"http-next"}`}}},
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: time.Millisecond, BatchSize: 10, HTTPBackoffInitial: time.Millisecond, HTTPBackoffMax: time.Millisecond},
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	for range 2 {
		if entry := writer.waitForInsert(t); entry.source != poller.RedisIngestSourceHTTPPull {
			t.Fatalf("expected HTTP source, got %q", entry.source)
		}
	}
	stop()
	waitForConnected(t, observer, 1)
}

func TestRedisIngestRunnerStartupAllFailedUsesTenSecondInitialRetry(t *testing.T) {
	logs := capturePollerLogs(t, logrus.DebugLevel)
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		&fakePullSource{err: errors.New("redis unavailable")},
		&fakePullSource{err: errors.New("http unavailable")},
		newFakeInboxWriter(),
		poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: 10},
	)
	stop := startRedisIngestTestRunner(t, runner)

	output := waitForLogContains(t, logs, "redis ingest startup retry scheduled", "retry_after=10s")
	stop()
	if !strings.Contains(output, "startup_failed") {
		t.Fatalf("expected startup failure before retry schedule, got logs: %s", output)
	}
}

func TestRedisIngestRunnerSubscribeBackfillsBeforeReceiving(t *testing.T) {
	observer := &controlObserverStub{}
	writer := newFakeInboxWriter()
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{{`{"request_id":"redis-backfill"}`}}},
		&fakePullSource{},
		writer,
		redisIngestTestConfig(10),
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	entry := writer.waitForInsert(t)
	stop()
	if entry.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected Redis backfill source, got %q", entry.source)
	}
	waitForConnected(t, observer, 1)
}

func TestRedisIngestRunnerWritesDynamicPullSourceName(t *testing.T) {
	writer := newFakeInboxWriter()
	redisSource := &fakeNamedPullSource{
		fakePullSource: &fakePullSource{batches: [][]string{{`{"request_id":"redis"}`}}},
		sourceName:     "redis_pull:queue",
	}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		redisSource,
		&fakePullSource{},
		writer,
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	entry := writer.waitForInsert(t)
	stop()
	if entry.source != "redis_pull:queue" {
		t.Fatalf("expected dynamic Redis pull source, got %q", entry.source)
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
		redisIngestTestConfig(1),
	)
	stop := startRedisIngestTestRunner(t, runner)

	first := writer.waitForInsert(t)
	second := writer.waitForInsert(t)
	stop()
	if first.source != poller.RedisIngestSourceRedisPull || second.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected Redis backfill source for both batches, got %q and %q", first.source, second.source)
	}
}

func TestRedisIngestRunnerSubscribeBackfillContinuesAfterFullControlOnlyBatch(t *testing.T) {
	delegate := newFakeInboxWriter()
	writer := poller.NewControlAwareRedisInboxWriter(delegate, &controlObserverStub{})
	sub := &blockingSubscription{messages: make(chan string)}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		&fakePullSource{batches: [][]string{
			{`{"refresh":true}`},
			{`{"request_id":"redis-after-control"}`},
		}},
		&fakePullSource{},
		writer,
		redisIngestTestConfig(1),
	)
	stop := startRedisIngestTestRunner(t, runner)

	entry := delegate.waitForInsert(t)
	stop()
	if entry.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected Redis backfill source, got %q", entry.source)
	}
	if len(entry.messages) != 1 || entry.messages[0] != `{"request_id":"redis-after-control"}` {
		t.Fatalf("expected usage after full control batch, got %+v", entry.messages)
	}
}

func TestRedisIngestRunnerSubscribeBackfillStopsAfterPartialControlOnlyBatch(t *testing.T) {
	delegate := newFakeInboxWriter()
	writer := poller.NewControlAwareRedisInboxWriter(delegate, &controlObserverStub{})
	sub := &blockingSubscription{messages: make(chan string)}
	redisSource := &fakePullSource{batches: [][]string{
		{`{"refresh":true}`},
		{`{"request_id":"should-not-pull"}`},
	}}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: sub},
		redisSource,
		&fakePullSource{},
		writer,
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	_ = waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastStatus == "subscribing"
	})
	stop()
	if calls := redisSource.callCount(); calls != 1 {
		t.Fatalf("expected backfill to stop after partial control-only batch, got %d calls", calls)
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
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	_ = writer.waitForInsert(t)
	output := waitForLogContains(t, logs, "redis subscribe backfill used redis pull")
	stop()
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
		redisIngestTestConfig(1),
	)
	stop := startRedisIngestTestRunner(t, runner)

	sub.messages <- `{"request_id":"subscribe"}`
	entry := writer.waitForInsert(t)
	waitForLogContains(t, logs, "redis subscribe messages received", "message_count=1", "inserted_count=1")
	stop()
	if entry.source != poller.RedisIngestSourceSubscribe {
		t.Fatalf("expected subscribe source, got %q", entry.source)
	}
}

func TestRedisIngestRunnerInfoLogsHTTPRecovery(t *testing.T) {
	logs := capturePollerLogs(t, logrus.InfoLevel)
	writer := newFakeInboxWriter()
	observer := &controlObserverStub{}
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
		redisIngestTestConfig(10),
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	initial := writer.waitForInsert(t)
	if initial.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected initial HTTP source, got %q", initial.source)
	}
	recovered := writer.waitForInsert(t)
	stop()
	if recovered.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected recovered HTTP source, got %q", recovered.source)
	}
	waitForConnected(t, observer, 2)
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
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	status := waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastStatus == "subscribing"
	})
	stop()
	if !status.SyncRunning {
		t.Fatalf("expected sync_running while subscribe is waiting, got status: %+v", status)
	}
}

func TestRedisIngestRunnerRedisPullRecoveryClearsStatusError(t *testing.T) {
	observer := &controlObserverStub{}
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
		redisIngestTestConfig(10),
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

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
	waitForConnected(t, observer, 2)
	stop()
}

func TestRedisIngestRunnerDegradedHTTPSuccessClearsStatusError(t *testing.T) {
	observer := &controlObserverStub{}
	writer := newFakeInboxWriter()
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: failingSubscription{err: io.EOF}},
		&fakePullSource{errs: []error{nil, errors.New("redis unavailable"), errors.New("redis unavailable"), errors.New("redis unavailable")}},
		&fakePullSource{batches: [][]string{{`{"request_id":"http-fallback"}`}}},
		writer,
		redisIngestTestConfig(10),
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	entry := writer.waitForInsert(t)
	if entry.source != poller.RedisIngestSourceHTTPPull {
		t.Fatalf("expected degraded HTTP source, got %q", entry.source)
	}
	_ = waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastError == "" && status.LastWarning == "" && status.SyncRunning
	})
	waitForConnected(t, observer, 2)
	stop()
}

func TestRedisIngestRunnerNotifiesMetadataAfterHTTPFallbackRecovery(t *testing.T) {
	observer := &controlObserverStub{}
	writer := newFakeInboxWriter()
	redisSource := &fakePullSource{
		errs:    []error{nil, errors.New("redis failed"), errors.New("redis failed")},
		batches: [][]string{{`{"request_id":"redis-initial"}`}},
	}
	httpSource := &fakePullSource{
		errs:    []error{nil, errors.New("http degraded failure"), nil},
		batches: [][]string{{`{"request_id":"http-fallback"}`}, {`{"request_id":"http-recovered"}`}},
	}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{err: errors.New("subscribe unavailable")},
		redisSource,
		httpSource,
		writer,
		poller.RedisIngestRunnerConfig{IdleInterval: time.Millisecond, BatchSize: 10, HTTPBackoffInitial: time.Millisecond, HTTPBackoffMax: time.Millisecond},
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	_ = writer.waitForInsert(t)
	_ = writer.waitForInsert(t)
	waitForConnected(t, observer, 3)
	stop()
}

func TestRedisIngestRunnerMarksMetadataPollingRequiredOnSubscribeDisconnect(t *testing.T) {
	observer := &controlObserverStub{}
	runner := poller.NewRedisIngestRunner(
		fakeSubscribeSource{sub: failingSubscription{err: io.EOF}},
		&fakePullSource{},
		&fakePullSource{},
		newFakeInboxWriter(),
		redisIngestTestConfig(10),
	)
	runner.SetControlMessageObserver(observer)
	stop := startRedisIngestTestRunner(t, runner)

	_ = waitForStatus(t, runner, func(status poller.Status) bool {
		return status.LastStatus == "subscribe_degraded_polling" || strings.Contains(status.LastError, "EOF")
	})
	stop()
	_, _, _, polling := observer.counts()
	if polling == 0 {
		t.Fatal("expected subscribe disconnect to restore metadata polling")
	}
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
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	attempt := writer.waitForAttempt(t)
	if attempt.source != poller.RedisIngestSourceRedisPull {
		t.Fatalf("expected failed write attempt from Redis source, got %q", attempt.source)
	}
	stop()
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
		redisIngestTestConfig(10),
	)
	stop := startRedisIngestTestRunner(t, runner)

	_ = writer.waitForInsert(t)
	stop()
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
	return s.sub, s.err
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

type fakeNamedPullSource struct {
	*fakePullSource
	sourceName string
}

func (s *fakeNamedPullSource) SourceName() string { return s.sourceName }

type fakeInboxInsert struct {
	source   string
	messages []string
}

type fakeInboxWriter struct {
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

func waitForConnected(t *testing.T, observer *controlObserverStub, expected int) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		connected, _, _, _ := observer.counts()
		if connected >= expected {
			if connected != expected {
				t.Fatalf("connection notifications = %d, want %d", connected, expected)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d connection notifications, got %d", expected, connected)
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

func redisIngestTestConfig(batchSize int) poller.RedisIngestRunnerConfig {
	return poller.RedisIngestRunnerConfig{IdleInterval: 10 * time.Millisecond, BatchSize: batchSize, HTTPBackoffInitial: 10 * time.Millisecond, HTTPBackoffMax: 10 * time.Millisecond}
}

func startRedisIngestTestRunner(t *testing.T, runner *poller.RedisIngestRunner) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.Run(ctx)
	}()
	stop := func() {
		t.Helper()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for runner to stop")
		}
	}
	t.Cleanup(stop)
	return stop
}
