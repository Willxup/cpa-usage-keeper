package cpa

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestRedisQueueClientPopsBatch(t *testing.T) {
	logs := captureRedisQueueClientInfoLogs(t)
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		if got := readRESPCommand(t, reader); strings.Join(got, " ") != cpaManagementRedisAuthCommand+" secret" {
			t.Fatalf("unexpected auth command: %v", got)
		}
		fmt.Fprint(conn, "+OK\r\n")
		if got := readRESPCommand(t, reader); strings.Join(got, " ") != cpaManagementRedisPopCommand+" "+ManagementUsageQueueKey+" 2" {
			t.Fatalf("unexpected pop command: %v", got)
		}
		fmt.Fprint(conn, "*2\r\n$7\r\n{\"a\":1}\r\n$7\r\n{\"b\":2}\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 2})
	messages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("PopUsage returned error: %v", err)
	}

	if len(messages) != 2 || messages[0] != `{"a":1}` || messages[1] != `{"b":2}` {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	if !strings.Contains(logs.String(), `msg="usage queue sync used redis protocol"`) {
		t.Fatalf("expected redis protocol log, got %q", logs.String())
	}
}

func TestRedisQueueClientFallsBackToHTTPUsageQueueWhenRedisFails(t *testing.T) {
	logs := captureRedisQueueClientInfoLogs(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cpaManagementUsageQueueEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Fatalf("expected count=2, got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"a":1},{"b":2}]`))
	}))
	defer server.Close()

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{
		BaseURL:       server.URL,
		RedisAddr:     "127.0.0.1:1",
		ManagementKey: "secret",
		Timeout:       10 * time.Millisecond,
		QueueKey:      ManagementUsageQueueKey,
		BatchSize:     2,
	})
	client.httpClient.httpClient = server.Client()
	messages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("PopUsage returned error: %v", err)
	}
	if len(messages) != 2 || messages[0] != `{"a":1}` || messages[1] != `{"b":2}` {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	content := logs.String()
	if !strings.Contains(content, `msg="usage queue sync used http protocol"`) {
		t.Fatalf("expected http fallback log, got %q", content)
	}
	if !strings.Contains(content, "redis_error=") {
		t.Fatalf("expected redis error field in fallback log, got %q", content)
	}
}

func TestRedisQueueClientCachesHTTPFallbackModeAfterFirstSuccessfulFallback(t *testing.T) {
	logs := captureRedisQueueClientInfoLogs(t)
	var httpCalls atomic.Int32
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls.Add(1)
		_, _ = w.Write([]byte(`[{"h":1}]`))
	}))
	defer httpServer.Close()

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{
		BaseURL:       httpServer.URL,
		RedisAddr:     "127.0.0.1:1",
		ManagementKey: "secret",
		Timeout:       10 * time.Millisecond,
		QueueKey:      ManagementUsageQueueKey,
		BatchSize:     1,
	})
	client.httpClient.httpClient = httpServer.Client()

	for range 2 {
		messages, err := client.PopUsage(ctxWithTimeout(t))
		if err != nil {
			t.Fatalf("PopUsage returned error: %v", err)
		}
		if len(messages) != 1 || messages[0] != `{"h":1}` {
			t.Fatalf("unexpected messages: %#v", messages)
		}
	}

	if httpCalls.Load() != 2 {
		t.Fatalf("expected cached http mode to make two http calls, got %d", httpCalls.Load())
	}
	if count := strings.Count(logs.String(), `msg="usage queue sync used http protocol"`); count != 1 {
		t.Fatalf("expected fallback mode to be logged once, got %d logs: %q", count, logs.String())
	}
}

func TestRedisQueueClientCachesRedisModeAfterFirstSuccessfulPop(t *testing.T) {
	logs := captureRedisQueueClientInfoLogs(t)
	var redisCalls atomic.Int32
	redisServer := newRedisQueueMultiTestServer(t, 2, func(t *testing.T, conn net.Conn) {
		redisCalls.Add(1)
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "*1\r\n$7\r\n{\"r\":1}\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: redisServer.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1})
	for range 2 {
		messages, err := client.PopUsage(ctxWithTimeout(t))
		if err != nil {
			t.Fatalf("PopUsage returned error: %v", err)
		}
		if len(messages) != 1 || messages[0] != `{"r":1}` {
			t.Fatalf("unexpected messages: %#v", messages)
		}
	}

	if redisCalls.Load() != 2 {
		t.Fatalf("expected cached redis mode to make two redis calls, got %d", redisCalls.Load())
	}
	if count := strings.Count(logs.String(), `msg="usage queue sync used redis protocol"`); count != 1 {
		t.Fatalf("expected redis mode to be logged once, got %d logs: %q", count, logs.String())
	}
}

func TestRedisQueueClientFallsBackToHTTPWhenCachedRedisModeFails(t *testing.T) {
	logs := captureRedisQueueClientInfoLogs(t)
	var redisCalls atomic.Int32
	redisServer := newRedisQueueMultiTestServer(t, 2, func(t *testing.T, conn net.Conn) {
		call := redisCalls.Add(1)
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		if call == 1 {
			fmt.Fprint(conn, "+OK\r\n")
			readRESPCommand(t, reader)
			fmt.Fprint(conn, "*1\r\n$7\r\n{\"r\":1}\r\n")
			return
		}
		fmt.Fprint(conn, "-ERR RESP AUTH disabled; use mTLS\r\n")
	})
	var httpCalls atomic.Int32
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		_, _ = w.Write([]byte(`[{"h":2}]`))
	}))
	defer httpServer.Close()

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{
		BaseURL:       httpServer.URL,
		RedisAddr:     redisServer.Addr,
		ManagementKey: "secret",
		Timeout:       time.Second,
		QueueKey:      ManagementUsageQueueKey,
		BatchSize:     1,
	})
	client.httpClient.httpClient = httpServer.Client()

	firstMessages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("first PopUsage returned error: %v", err)
	}
	if len(firstMessages) != 1 || firstMessages[0] != `{"r":1}` {
		t.Fatalf("unexpected first messages: %#v", firstMessages)
	}

	secondMessages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("second PopUsage returned error: %v", err)
	}
	if len(secondMessages) != 1 || secondMessages[0] != `{"h":2}` {
		t.Fatalf("unexpected second messages: %#v", secondMessages)
	}
	if httpCalls.Load() != 1 {
		t.Fatalf("expected cached redis failure to fall back to one http call, got %d", httpCalls.Load())
	}
	content := logs.String()
	if !strings.Contains(content, `msg="usage queue sync used http protocol"`) {
		t.Fatalf("expected http fallback log, got %q", content)
	}
}

func TestRedisQueueClientPrefersRedisBeforeHTTPFallback(t *testing.T) {
	redisServer := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "*1\r\n$7\r\n{\"r\":1}\r\n")
	})
	httpCalled := false
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalled = true
		_, _ = w.Write([]byte(`[{"h":1}]`))
	}))
	defer httpServer.Close()

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{
		BaseURL:       httpServer.URL,
		RedisAddr:     redisServer.URL,
		ManagementKey: "secret",
		Timeout:       time.Second,
		QueueKey:      ManagementUsageQueueKey,
		BatchSize:     2,
	})
	client.httpClient.httpClient = httpServer.Client()
	messages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("PopUsage returned error: %v", err)
	}
	if httpCalled {
		t.Fatal("expected redis success to skip http fallback")
	}
	if len(messages) != 1 || messages[0] != `{"r":1}` {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

func TestRedisQueueClientTreatsEmptyPopAsSuccess(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "*0\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1000})
	messages, err := client.PopUsage(ctxWithTimeout(t))
	if err != nil {
		t.Fatalf("PopUsage returned error: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected empty messages, got %#v", messages)
	}
}

func TestRedisQueueClientClassifiesAuthErrors(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		readRESPCommand(t, bufio.NewReader(conn))
		fmt.Fprint(conn, "-ERR invalid password\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "wrong", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1000})
	_, err := client.PopUsage(ctxWithTimeout(t))
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !errors.Is(err, ErrRedisQueueAuth) {
		t.Fatalf("expected ErrRedisQueueAuth, got %v", err)
	}
}

func TestRedisQueueClientTLS(t *testing.T) {
	cases := []struct {
		name      string
		configure func(opts *RedisQueueOptions, server redisQueueTestServer)
		response  string
		expected  []string
	}{
		{
			name: "auto-detected from https base URL",
			configure: func(opts *RedisQueueOptions, server redisQueueTestServer) {
				opts.BaseURL = server.URL
			},
			response: "*1\r\n$5\r\nhello\r\n",
			expected: []string{"hello"},
		},
		{
			name: "explicit TLS option with redis addr",
			configure: func(opts *RedisQueueOptions, server redisQueueTestServer) {
				opts.RedisAddr = server.Addr
				opts.TLS = true
			},
			response: "*0\r\n",
			expected: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newRedisQueueTLSTestServer(t, func(t *testing.T, conn net.Conn) {
				reader := bufio.NewReader(conn)
				readRESPCommand(t, reader)
				fmt.Fprint(conn, "+OK\r\n")
				readRESPCommand(t, reader)
				fmt.Fprint(conn, tc.response)
			})

			opts := RedisQueueOptions{
				ManagementKey: "secret",
				Timeout:       time.Second,
				QueueKey:      ManagementUsageQueueKey,
				BatchSize:     1,
				TLSSkipVerify: true,
			}
			tc.configure(&opts, server)

			client := NewRedisQueueClientWithOptions(opts)
			messages, err := client.PopUsage(ctxWithTimeout(t))
			if err != nil {
				t.Fatalf("PopUsage over TLS returned error: %v", err)
			}
			if len(messages) != len(tc.expected) {
				t.Fatalf("expected %d messages, got %#v", len(tc.expected), messages)
			}
			for i, want := range tc.expected {
				if messages[i] != want {
					t.Fatalf("message[%d] = %q, want %q", i, messages[i], want)
				}
			}
		})
	}
}

func TestRedisQueueClientPrefersExplicitQueueAddr(t *testing.T) {
	if got, tls := redisQueueAddress("https://cpa.example.com", "redis-stream.example.com:6380"); got != "redis-stream.example.com:6380" || tls {
		t.Fatalf("expected explicit redis queue addr without TLS, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("https://cpa.example.com", "redis://redis-stream.example.com:6380"); got != "redis-stream.example.com:6380" || tls {
		t.Fatalf("expected redis scheme to be stripped without TLS, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("https://cpa.example.com", "rediss://redis-stream.example.com:6380"); got != "redis-stream.example.com:6380" || !tls {
		t.Fatalf("expected rediss scheme to enable TLS, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("https://cpa.example.com", "http://redis-stream.example.com:6380"); got != "redis-stream.example.com:6380" || tls {
		t.Fatalf("expected http scheme to be stripped without TLS, got %q tls=%v", got, tls)
	}
}

func TestRedisQueueClientDefaultsToManagementPortFromBaseURLHost(t *testing.T) {
	if got, tls := redisQueueAddress("https://cpa.example.com", ""); got != "cpa.example.com:"+ManagementRedisDefaultPort || !tls {
		t.Fatalf("expected default management port with TLS from https host, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("http://cpa.example.com", ""); got != "cpa.example.com:"+ManagementRedisDefaultPort || tls {
		t.Fatalf("expected default management port without TLS from http host, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("https://127.0.0.1:"+ManagementRedisDefaultPort, ""); got != "127.0.0.1:"+ManagementRedisDefaultPort || !tls {
		t.Fatalf("expected explicit port with TLS to be preserved, got %q tls=%v", got, tls)
	}
	if got, tls := redisQueueAddress("http://127.0.0.1:"+ManagementRedisDefaultPort, ""); got != "127.0.0.1:"+ManagementRedisDefaultPort || tls {
		t.Fatalf("expected explicit port without TLS to be preserved, got %q tls=%v", got, tls)
	}
}

func TestRedisQueueClientReportsMalformedRESP(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "!not-resp\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1000})
	_, err := client.PopUsage(ctxWithTimeout(t))
	if err == nil || !strings.Contains(err.Error(), "read redis queue pop response") {
		t.Fatalf("expected malformed response error, got %v", err)
	}
}

type redisQueueTestServer struct {
	URL  string
	Addr string
}

func newRedisQueueTestServer(t *testing.T, handler func(*testing.T, net.Conn)) redisQueueTestServer {
	return startRedisQueueTestServer(t, false, handler)
}

func newRedisQueueTLSTestServer(t *testing.T, handler func(*testing.T, net.Conn)) redisQueueTestServer {
	return startRedisQueueTestServer(t, true, handler)
}

func startRedisQueueTestServer(t *testing.T, useTLS bool, handler func(*testing.T, net.Conn)) redisQueueTestServer {
	t.Helper()
	return startRedisQueueMultiTestServer(t, 1, useTLS, handler)
}

func newRedisQueueMultiTestServer(t *testing.T, connections int, handler func(*testing.T, net.Conn)) redisQueueTestServer {
	t.Helper()
	return startRedisQueueMultiTestServer(t, connections, false, handler)
}

func startRedisQueueMultiTestServer(t *testing.T, connections int, useTLS bool, handler func(*testing.T, net.Conn)) redisQueueTestServer {
	t.Helper()
	var listener net.Listener
	var err error
	if useTLS {
		cert := generateSelfSignedCert(t)
		listener, err = tls.Listen(cpaManagementRedisNetwork, "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	} else {
		listener, err = net.Listen(cpaManagementRedisNetwork, "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range connections {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handler(t, conn)
			conn.Close()
		}
	}()
	t.Cleanup(func() { <-done })

	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	addr := listener.Addr().String()
	return redisQueueTestServer{URL: scheme + "://" + addr, Addr: addr}
}

func generateSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

func readRESPCommand(t *testing.T, reader *bufio.Reader) []string {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read command header: %v", err)
	}
	var count int
	if _, err := fmt.Sscanf(line, "*%d\r\n", &count); err != nil {
		t.Fatalf("parse command header %q: %v", line, err)
	}
	parts := make([]string, 0, count)
	for range count {
		bulkHeader, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read bulk header: %v", err)
		}
		var size int
		if _, err := fmt.Sscanf(bulkHeader, "$%d\r\n", &size); err != nil {
			t.Fatalf("parse bulk header %q: %v", bulkHeader, err)
		}
		buf := make([]byte, size+2)
		if _, err := reader.Read(buf); err != nil {
			t.Fatalf("read bulk body: %v", err)
		}
		parts = append(parts, string(buf[:size]))
	}
	return parts
}

func captureRedisQueueClientInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func ctxWithTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}
