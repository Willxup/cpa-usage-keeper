package poller_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/poller"
)

func TestRedisErrorSubscribeSourceUsesDedicatedErrorsChannel(t *testing.T) {
	payload := `{"timestamp":"2026-08-20T12:00:00Z","auth_index":"auth-a"}`
	server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		if got := readRESPCommandForPollerTest(t, reader); strings.Join(got, " ") != cpa.ManagementRedisAuthCommand+" secret" {
			t.Fatalf("unexpected auth command: %v", got)
		}
		fmt.Fprint(conn, "+OK\r\n")
		if got := readRESPCommandForPollerTest(t, reader); strings.Join(got, " ") != cpa.ManagementRedisSubscribeCommand+" "+cpa.ManagementErrorsSubscribeChannel {
			t.Fatalf("unexpected subscribe command: %v", got)
		}
		fmt.Fprintf(conn, "*3\r\n$9\r\nsubscribe\r\n$6\r\nerrors\r\n:1\r\n")
		fmt.Fprintf(conn, "*3\r\n$7\r\nmessage\r\n$6\r\nerrors\r\n$%d\r\n%s\r\n", len(payload), payload)
	})

	source := poller.NewRedisErrorSubscribeSource(poller.RedisSubscribeOptions{RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second})
	subscription, err := source.SubscribeErrors(context.Background())
	if err != nil {
		t.Fatalf("SubscribeErrors returned error: %v", err)
	}
	defer subscription.Close()
	message, err := subscription.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive returned error: %v", err)
	}
	if message != payload {
		t.Fatalf("message = %q, want %q", message, payload)
	}
}

func TestRedisErrorSubscribeSourceClassifiesUnsupportedChannel(t *testing.T) {
	server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommandForPollerTest(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommandForPollerTest(t, reader)
		fmt.Fprint(conn, "-ERR unsupported channel 'errors'\r\n")
	})

	source := poller.NewRedisErrorSubscribeSource(poller.RedisSubscribeOptions{RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second})
	_, err := source.SubscribeErrors(context.Background())
	if !errors.Is(err, poller.ErrRedisErrorSubscribeUnsupported) {
		t.Fatalf("expected unsupported sentinel, got %v", err)
	}
}

func TestRedisErrorSubscribeSourceCancelsBlockedHandshake(t *testing.T) {
	authReceived := make(chan struct{})
	server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommandForPollerTest(t, reader)
		close(authReceived)
		// CPA 故意不回复 AUTH；客户端取消 context 后必须关闭自己的专用连接来唤醒这里。
		var buffer [1]byte
		_, _ = conn.Read(buffer[:])
	})

	source := poller.NewRedisErrorSubscribeSource(poller.RedisSubscribeOptions{
		RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := source.SubscribeErrors(ctx)
		result <- err
	}()
	<-authReceived
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SubscribeErrors error = %v, want context.Canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("SubscribeErrors did not stop promptly after context cancellation")
	}
}

func TestRedisErrorIngestDefaultRetrySchedule(t *testing.T) {
	want := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 3 * time.Minute, 10 * time.Minute}
	if got := poller.DefaultRedisErrorRetryDelays(); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry delays = %v, want %v", got, want)
	}
}

func TestRedisErrorIngestStopsAfterFiveRetriesWithoutReturningError(t *testing.T) {
	source := &fakeErrorSubscribeSource{err: errors.New("network unavailable")}
	runner := poller.NewRedisErrorIngestRunnerWithOptions(source, &fakeErrorEventSink{}, poller.RedisErrorIngestRunnerOptions{
		RetryDelays: []time.Duration{0, 0, 0, 0, 0},
	})
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run must not propagate optional errors failure: %v", err)
	}
	if source.calls != 6 {
		t.Fatalf("SubscribeErrors calls = %d, want initial attempt plus five retries", source.calls)
	}
}

func TestRedisErrorIngestDoesNotRetryPermanentSubscribeFailure(t *testing.T) {
	for _, permanentErr := range []error{poller.ErrRedisErrorSubscribeUnsupported, cpa.ErrRedisQueueAuth} {
		source := &fakeErrorSubscribeSource{err: permanentErr}
		runner := poller.NewRedisErrorIngestRunnerWithOptions(source, &fakeErrorEventSink{}, poller.RedisErrorIngestRunnerOptions{
			RetryDelays: []time.Duration{0, 0, 0, 0, 0},
		})
		if err := runner.Run(context.Background()); err != nil {
			t.Fatalf("Run returned error for %v: %v", permanentErr, err)
		}
		if source.calls != 1 {
			t.Fatalf("SubscribeErrors calls for %v = %d, want 1", permanentErr, source.calls)
		}
	}
}

func TestRedisErrorIngestContinuesAfterWriteFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	subscription := &fakeErrorSubscription{messages: []string{"first", "second"}}
	source := &fakeErrorSubscribeSource{subscription: subscription}
	sink := &fakeErrorEventSink{failFirst: true, afterStore: func(call int) {
		if call == 2 {
			cancel()
		}
	}}
	runner := poller.NewRedisErrorIngestRunnerWithOptions(source, sink, poller.RedisErrorIngestRunnerOptions{
		RetryDelays: []time.Duration{0, 0, 0, 0, 0},
	})
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if sink.calls != 2 {
		t.Fatalf("sink calls = %d, want second message after first write failure", sink.calls)
	}
}

type fakeErrorSubscribeSource struct {
	mu           sync.Mutex
	calls        int
	err          error
	subscription poller.ErrorSubscription
}

func (s *fakeErrorSubscribeSource) SubscribeErrors(context.Context) (poller.ErrorSubscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.subscription, nil
}

type fakeErrorSubscription struct {
	mu       sync.Mutex
	messages []string
	index    int
}

func (s *fakeErrorSubscription) Receive(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.index < len(s.messages) {
		message := s.messages[s.index]
		s.index++
		s.mu.Unlock()
		return message, nil
	}
	s.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (s *fakeErrorSubscription) Close() error { return nil }

type fakeErrorEventSink struct {
	mu         sync.Mutex
	calls      int
	failFirst  bool
	afterStore func(int)
}

func (s *fakeErrorEventSink) StoreErrorEvent(context.Context, string, time.Time) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	fail := s.failFirst && call == 1
	afterStore := s.afterStore
	s.mu.Unlock()
	if afterStore != nil {
		afterStore(call)
	}
	if fail {
		return errors.New("database write failed")
	}
	return nil
}
