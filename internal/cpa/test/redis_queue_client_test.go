package cpa_test

import (
	"bufio"
	"context"
	. "cpa-usage-keeper/internal/cpa"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
	_ "unsafe"
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
			if !slices.Equal(messages, tc.expected) {
				t.Fatalf("messages = %v, want %v", messages, tc.expected)
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

func TestRedisQueueClientRejectsInvalidRESP(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		batchSize      int
		wantError      string
	}{
		{"oversized bulk", "$4194305\r\n", 1000, "exceeds maximum size"},
		{"array larger than batch", "*2\r\n$2\r\n{}\r\n$2\r\n{}\r\n", 1, "array exceeds maximum length"},
		{"oversized array", "*10001\r\n", 1000, "array exceeds maximum length"},
		{"malformed response", "!not-resp\r\n", 1000, "read redis queue pop response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newRedisQueueTestServer(t, func(t *testing.T, conn net.Conn) {
				reader := bufio.NewReader(conn)
				readRESPCommand(t, reader)
				fmt.Fprint(conn, "+OK\r\n")
				readRESPCommand(t, reader)
				fmt.Fprint(conn, tc.response)
			})
			client := NewRedisQueueClientWithOptions(RedisQueueOptions{
				BaseURL: server.URL, ManagementKey: "secret", Timeout: time.Second,
				QueueKey: ManagementUsageQueueKey, BatchSize: tc.batchSize,
			})
			if _, err := client.PopUsage(ctxWithTimeout(t)); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("PopUsage error = %v, want %q", err, tc.wantError)
			}
		})
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
	var listener net.Listener
	var err error
	if useTLS {
		cert := generateSelfSignedCert(t)
		listener, err = tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	} else {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handler(t, conn)
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
	})

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
		if _, err := io.ReadFull(reader, buf); err != nil {
			t.Fatalf("read bulk body: %v", err)
		}
		parts = append(parts, string(buf[:size]))
	}
	return parts
}

func ctxWithTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

// 地址解析使用原函数，保留显式 Redis TLS 与默认管理端口的对应关系。
//
//go:linkname redisQueueAddress cpa-usage-keeper/internal/cpa.redisQueueAddress
func redisQueueAddress(baseURL, redisQueueAddr string) (string, bool)
