package poller_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/poller"
)

func TestRedisSubscribeSourceRejectsSubscribeError(t *testing.T) {
	server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		if got := readRESPCommandForPollerTest(t, reader); strings.Join(got, " ") != cpa.ManagementRedisAuthCommand+" secret" {
			t.Fatalf("unexpected auth command: %v", got)
		}
		fmt.Fprint(conn, "+OK\r\n")
		if got := readRESPCommandForPollerTest(t, reader); strings.Join(got, " ") != cpa.ManagementRedisSubscribeCommand+" "+cpa.ManagementUsageSubscribeChannel {
			t.Fatalf("unexpected subscribe command: %v", got)
		}
		fmt.Fprint(conn, "-ERR subscribe disabled\r\n")
	})

	source := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second})
	_, err := source.Subscribe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "subscribe disabled") {
		t.Fatalf("expected subscribe error, got %v", err)
	}
}

func TestRedisSubscriptionReceiveHonorsContextCancel(t *testing.T) {
	for name, newContext := range map[string]func(context.Context) (context.Context, context.CancelFunc){
		"without deadline": context.WithCancel,
		"with deadline": func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			defer close(done)
			server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
				reader := bufio.NewReader(conn)
				readRESPCommandForPollerTest(t, reader)
				fmt.Fprint(conn, "+OK\r\n")
				readRESPCommandForPollerTest(t, reader)
				fmt.Fprint(conn, redisSubscribeAckForPollerTest())
				<-done
			})

			source := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second})
			sub, err := source.Subscribe(context.Background())
			if err != nil {
				t.Fatalf("Subscribe returned error: %v", err)
			}
			defer sub.Close()

			ctx, cancel := newContext(context.Background())
			defer cancel()
			errCh := make(chan error, 1)
			go func() {
				_, err := sub.Receive(ctx)
				errCh <- err
			}()
			cancel()

			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context.Canceled, got %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for canceled receive")
			}
		})
	}
}

func TestRedisSubscriptionReceiveHonorsContextDeadline(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	server := newRESPServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommandForPollerTest(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommandForPollerTest(t, reader)
		fmt.Fprint(conn, redisSubscribeAckForPollerTest())
		fmt.Fprint(conn, redisMessageForPollerTest(`{"request_id":"x"}`))
		<-done
	})

	source := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{RedisAddr: server.addr, ManagementKey: "secret", Timeout: time.Second})
	sub, err := source.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer sub.Close()

	message, err := sub.Receive(context.Background())
	if err != nil {
		t.Fatalf("first Receive returned error: %v", err)
	}
	if message != `{"request_id":"x"}` {
		t.Fatalf("unexpected message %q", message)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = sub.Receive(ctx)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected receive to honor context deadline, got %v", err)
	}
}

type respTestServer struct {
	addr string
}

func newRESPServer(t *testing.T, handler func(*testing.T, net.Conn)) respTestServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := respTestServer{addr: listener.Addr().String()}
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handler(t, conn)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
	})
	return server
}

func redisSubscribeAckForPollerTest() string {
	channel := cpa.ManagementUsageSubscribeChannel
	return fmt.Sprintf("*3\r\n$9\r\nsubscribe\r\n$%d\r\n%s\r\n:1\r\n", len(channel), channel)
}

func redisMessageForPollerTest(payload string) string {
	channel := cpa.ManagementUsageSubscribeChannel
	return fmt.Sprintf("*3\r\n$7\r\nmessage\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(channel), channel, len(payload), payload)
}

func readRESPCommandForPollerTest(t *testing.T, reader *bufio.Reader) []string {
	t.Helper()
	parts, err := readRedisPullRESPCommand(reader)
	if err != nil {
		t.Fatalf("read RESP command: %v", err)
	}
	return parts
}
