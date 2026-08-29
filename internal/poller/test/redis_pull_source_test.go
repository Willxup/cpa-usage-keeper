package poller_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/poller"
)

//go:linkname redisPullQueueKeyCandidates cpa-usage-keeper/internal/poller.redisPullQueueKeyCandidates
func redisPullQueueKeyCandidates() []string

func TestRedisPullSourceFallsBackToLegacyQueueKeyAndReusesSelection(t *testing.T) {
	addr, wait := newRedisPullScriptedServer(t, []redisPullExpectation{
		{queueKey: cpa.ManagementUsageQueueKey, response: "-ERR unsupported channel 'usage'\r\n"},
		{queueKey: cpa.ManagementUsageLegacyQueueKey, response: redisPullArrayResponse("legacy-one")},
		{queueKey: cpa.ManagementUsageLegacyQueueKey, response: redisPullArrayResponse("legacy-two")},
	})

	source := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		RedisAddr:     addr,
		ManagementKey: "secret",
		Timeout:       time.Second,
		BatchSize:     1,
	})

	messages, err := source.Pull(context.Background())
	if err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}
	if len(messages) != 1 || messages[0] != "legacy-one" {
		t.Fatalf("unexpected first messages: %#v", messages)
	}
	if source.SourceName() != "redis_pull:queue" {
		t.Fatalf("expected legacy source name redis_pull:queue, got %q", source.SourceName())
	}

	messages, err = source.Pull(context.Background())
	if err != nil {
		t.Fatalf("second Pull returned error: %v", err)
	}
	if len(messages) != 1 || messages[0] != "legacy-two" {
		t.Fatalf("unexpected second messages: %#v", messages)
	}

	wait()
}

func TestRedisPullSourceLocksUsageQueueKeyAfterEmptySuccess(t *testing.T) {
	addr, wait := newRedisPullScriptedServer(t, []redisPullExpectation{
		{queueKey: cpa.ManagementUsageQueueKey, response: "*0\r\n"},
		{queueKey: cpa.ManagementUsageQueueKey, response: redisPullArrayResponse("usage-one")},
	})

	source := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		RedisAddr:     addr,
		ManagementKey: "secret",
		Timeout:       time.Second,
		BatchSize:     1,
	})

	messages, err := source.Pull(context.Background())
	if err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected empty first messages, got %#v", messages)
	}
	if source.SourceName() != "redis_pull:usage" {
		t.Fatalf("expected usage source name redis_pull:usage, got %q", source.SourceName())
	}

	messages, err = source.Pull(context.Background())
	if err != nil {
		t.Fatalf("second Pull returned error: %v", err)
	}
	if len(messages) != 1 || messages[0] != "usage-one" {
		t.Fatalf("unexpected second messages: %#v", messages)
	}

	wait()
}

func TestRedisPullSourceNameDoesNotWaitForSelectedPop(t *testing.T) {
	addr, releaseSecondPop, waitForSecondPop, wait := newRedisPullBlockingSecondPopServer(t)

	source := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		RedisAddr:     addr,
		ManagementKey: "secret",
		Timeout:       time.Second,
		BatchSize:     1,
	})

	messages, err := source.Pull(context.Background())
	if err != nil {
		t.Fatalf("first Pull returned error: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected empty first messages, got %#v", messages)
	}

	pullDone := make(chan error, 1)
	go func() {
		_, err := source.Pull(context.Background())
		pullDone <- err
	}()

	waitForSecondPop()

	sourceNameDone := make(chan string, 1)
	go func() {
		sourceNameDone <- source.SourceName()
	}()

	select {
	case sourceName := <-sourceNameDone:
		if sourceName != "redis_pull:usage" {
			t.Fatalf("expected source name redis_pull:usage, got %q", sourceName)
		}
	case <-time.After(100 * time.Millisecond):
		releaseSecondPop()
		if err := <-pullDone; err != nil {
			t.Fatalf("second Pull returned error after release: %v", err)
		}
		t.Fatal("SourceName waited for selected PopUsage to finish")
	}

	releaseSecondPop()
	if err := <-pullDone; err != nil {
		t.Fatalf("second Pull returned error: %v", err)
	}
	wait()
}

func TestRedisPullSourceStopsAfterTwoUnsupportedQueueKeyAttempts(t *testing.T) {
	addr, wait := newRedisPullScriptedServer(t, []redisPullExpectation{
		{queueKey: cpa.ManagementUsageQueueKey, response: "-ERR unsupported channel 'usage'\r\n"},
		{queueKey: cpa.ManagementUsageLegacyQueueKey, response: "-ERR unsupported channel 'queue'\r\n"},
	})

	source := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		RedisAddr:     addr,
		ManagementKey: "secret",
		Timeout:       time.Second,
		BatchSize:     1,
	})

	_, err := source.Pull(context.Background())
	if err == nil {
		t.Fatal("expected Pull to return error")
	}
	errorText := err.Error()
	if !strings.Contains(errorText, "usage") || !strings.Contains(errorText, "queue") {
		t.Fatalf("expected joined error to include both attempted keys, got %q", errorText)
	}

	wait()
}

func TestRedisPullQueueKeyCandidatesAreUsageThenQueue(t *testing.T) {
	got := redisPullQueueKeyCandidates()
	want := []string{cpa.ManagementUsageQueueKey, cpa.ManagementUsageLegacyQueueKey}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("redisPullQueueKeyCandidates() = %#v, want %#v", got, want)
	}
	if len(got) != 2 {
		t.Fatalf("redisPullQueueKeyCandidates() returned %d keys, want 2", len(got))
	}
}

type redisPullExpectation struct {
	queueKey string
	response string
}

func newRedisPullScriptedServer(t *testing.T, expectations []redisPullExpectation) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen redis pull test server: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		for i, expectation := range expectations {
			conn, err := listener.Accept()
			if err != nil {
				done <- fmt.Errorf("accept connection %d: %w", i+1, err)
				return
			}
			if err := handleRedisPullExpectation(conn, expectation); err != nil {
				done <- fmt.Errorf("connection %d: %w", i+1, err)
				return
			}
		}
	}()

	wait := func() {
		t.Helper()
		if err := listener.Close(); err != nil {
			t.Fatalf("close redis pull test listener: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for redis pull test server")
		}
	}
	t.Cleanup(func() { _ = listener.Close() })

	return listener.Addr().String(), wait
}

func newRedisPullBlockingSecondPopServer(t *testing.T) (string, func(), func(), func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen redis pull blocking test server: %v", err)
	}

	secondPopReady := make(chan struct{})
	releaseSecondPop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		defer close(done)
		expectations := []redisPullExpectation{
			{queueKey: cpa.ManagementUsageQueueKey, response: "*0\r\n"},
			{queueKey: cpa.ManagementUsageQueueKey, response: redisPullArrayResponse("usage-after-release")},
		}
		for i, expectation := range expectations {
			conn, err := listener.Accept()
			if err != nil {
				done <- fmt.Errorf("accept blocking connection %d: %w", i+1, err)
				return
			}
			if i == 1 {
				if err := handleRedisPullExpectationAfterSignal(conn, expectation, secondPopReady, releaseSecondPop); err != nil {
					done <- fmt.Errorf("blocking connection %d: %w", i+1, err)
					return
				}
				continue
			}
			if err := handleRedisPullExpectation(conn, expectation); err != nil {
				done <- fmt.Errorf("blocking connection %d: %w", i+1, err)
				return
			}
		}
	}()

	release := func() {
		t.Helper()
		select {
		case <-releaseSecondPop:
		default:
			close(releaseSecondPop)
		}
	}
	waitForPop := func() {
		t.Helper()
		select {
		case <-secondPopReady:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for second redis pop command")
		}
	}
	wait := func() {
		t.Helper()
		if err := listener.Close(); err != nil {
			t.Fatalf("close redis pull blocking test listener: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for redis pull blocking test server")
		}
	}
	t.Cleanup(func() { _ = listener.Close() })

	return listener.Addr().String(), release, waitForPop, wait
}

func handleRedisPullExpectation(conn net.Conn, expectation redisPullExpectation) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	authCommand, err := readRedisPullRESPCommand(reader)
	if err != nil {
		return fmt.Errorf("read auth command: %w", err)
	}
	if got, want := strings.Join(authCommand, " "), cpa.ManagementRedisAuthCommand+" secret"; got != want {
		return fmt.Errorf("auth command = %q, want %q", got, want)
	}
	if _, err := fmt.Fprint(conn, "+OK\r\n"); err != nil {
		return fmt.Errorf("write auth response: %w", err)
	}

	popCommand, err := readRedisPullRESPCommand(reader)
	if err != nil {
		return fmt.Errorf("read pop command: %w", err)
	}
	wantPopCommand := cpa.ManagementRedisPopCommand + " " + expectation.queueKey + " 1"
	if got := strings.Join(popCommand, " "); got != wantPopCommand {
		return fmt.Errorf("pop command = %q, want %q", got, wantPopCommand)
	}
	if _, err := fmt.Fprint(conn, expectation.response); err != nil {
		return fmt.Errorf("write pop response: %w", err)
	}
	return nil
}

func handleRedisPullExpectationAfterSignal(conn net.Conn, expectation redisPullExpectation, ready chan<- struct{}, release <-chan struct{}) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	authCommand, err := readRedisPullRESPCommand(reader)
	if err != nil {
		return fmt.Errorf("read auth command: %w", err)
	}
	if got, want := strings.Join(authCommand, " "), cpa.ManagementRedisAuthCommand+" secret"; got != want {
		return fmt.Errorf("auth command = %q, want %q", got, want)
	}
	if _, err := fmt.Fprint(conn, "+OK\r\n"); err != nil {
		return fmt.Errorf("write auth response: %w", err)
	}

	popCommand, err := readRedisPullRESPCommand(reader)
	if err != nil {
		return fmt.Errorf("read pop command: %w", err)
	}
	wantPopCommand := cpa.ManagementRedisPopCommand + " " + expectation.queueKey + " 1"
	if got := strings.Join(popCommand, " "); got != wantPopCommand {
		return fmt.Errorf("pop command = %q, want %q", got, wantPopCommand)
	}
	close(ready)
	<-release

	if _, err := fmt.Fprint(conn, expectation.response); err != nil {
		return fmt.Errorf("write pop response: %w", err)
	}
	return nil
}

func readRedisPullRESPCommand(reader *bufio.Reader) ([]string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if prefix != '*' {
		return nil, fmt.Errorf("expected RESP array, got %q", prefix)
	}
	countLine, err := readRedisPullRESPLine(reader)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(countLine)
	if err != nil || count < 0 {
		return nil, fmt.Errorf("invalid RESP array length %q", countLine)
	}

	parts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		value, err := readRedisPullRESPBulkString(reader)
		if err != nil {
			return nil, err
		}
		parts = append(parts, value)
	}
	return parts, nil
}

func readRedisPullRESPBulkString(reader *bufio.Reader) (string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	if prefix != '$' {
		return "", fmt.Errorf("expected RESP bulk string, got %q", prefix)
	}
	lengthLine, err := readRedisPullRESPLine(reader)
	if err != nil {
		return "", err
	}
	length, err := strconv.Atoi(lengthLine)
	if err != nil || length < 0 {
		return "", fmt.Errorf("invalid RESP bulk length %q", lengthLine)
	}

	payload := make([]byte, length+2)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", err
	}
	if payload[length] != '\r' || payload[length+1] != '\n' {
		return "", fmt.Errorf("malformed RESP bulk string")
	}
	return string(payload[:length]), nil
}

func readRedisPullRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(line, "\r\n") {
		return "", fmt.Errorf("malformed RESP line")
	}
	return strings.TrimSuffix(line, "\r\n"), nil
}

func redisPullArrayResponse(value string) string {
	return fmt.Sprintf("*1\r\n$%d\r\n%s\r\n", len(value), value)
}
