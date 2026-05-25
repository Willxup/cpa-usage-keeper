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
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestRedisQueueClientPopsBatch(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		if got := readRESPCommand(t, reader); strings.Join(got, " ") != ManagementRedisAuthCommand+" secret" {
			t.Fatalf("unexpected auth command: %v", got)
		}
		fmt.Fprint(conn, "+OK\r\n")
		if got := readRESPCommand(t, reader); strings.Join(got, " ") != ManagementRedisPopCommand+" "+ManagementUsageQueueKey+" 2" {
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

func TestRedisQueueClientRejectsOversizedRESPBulk(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "$4194305\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1000})
	_, err := client.PopUsage(ctxWithTimeout(t))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected oversized bulk error, got %v", err)
	}
}

func TestRedisQueueClientRejectsArrayLargerThanBatchSize(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "*2\r\n$2\r\n{}\r\n$2\r\n{}\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1})
	_, err := client.PopUsage(ctxWithTimeout(t))
	if err == nil || !strings.Contains(err.Error(), "array exceeds maximum length") {
		t.Fatalf("expected batch-size array limit error, got %v", err)
	}
}

func TestRedisQueueClientRejectsOversizedRESPArray(t *testing.T) {
	server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
		reader := bufio.NewReader(conn)
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "+OK\r\n")
		readRESPCommand(t, reader)
		fmt.Fprint(conn, "*10001\r\n")
	})

	client := NewRedisQueueClientWithOptions(RedisQueueOptions{BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second, QueueKey: ManagementUsageQueueKey, BatchSize: 1000})
	_, err := client.PopUsage(ctxWithTimeout(t))
	if err == nil || !strings.Contains(err.Error(), "array exceeds maximum length") {
		t.Fatalf("expected oversized array error, got %v", err)
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
