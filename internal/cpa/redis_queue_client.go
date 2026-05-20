package cpa

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var ErrRedisQueueAuth = errors.New("redis queue auth failed")

type redisQueueSyncMode string

const (
	redisQueueSyncModeRedis redisQueueSyncMode = "redis"
	redisQueueSyncModeHTTP  redisQueueSyncMode = "http"
)

type RedisQueueClient struct {
	address       string
	managementKey string
	timeout       time.Duration
	queueKey      string
	batchSize     int
	httpClient    *Client
	mu            sync.Mutex
	syncMode      redisQueueSyncMode
	dial          func(ctx context.Context, network, addr string) (net.Conn, error)
}

type RedisQueueOptions struct {
	BaseURL       string
	RedisAddr     string
	ManagementKey string
	Timeout       time.Duration
	QueueKey      string
	BatchSize     int
	TLS           bool
	TLSSkipVerify bool
}

func NewRedisQueueClientWithOptions(opts RedisQueueOptions) *RedisQueueClient {
	addr, useTLS := redisQueueAddress(opts.BaseURL, opts.RedisAddr)
	if opts.TLS {
		useTLS = true
	}
	netDialer := &net.Dialer{Timeout: opts.Timeout}
	dial := netDialer.DialContext
	if useTLS {
		tlsDialer := &tls.Dialer{NetDialer: netDialer, Config: &tls.Config{InsecureSkipVerify: opts.TLSSkipVerify}}
		dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if opts.Timeout > 0 {
				deadline := time.Now().Add(opts.Timeout)
				if existing, ok := ctx.Deadline(); !ok || deadline.Before(existing) {
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, deadline)
					defer cancel()
				}
			}
			return tlsDialer.DialContext(ctx, network, addr)
		}
	}
	return &RedisQueueClient{
		address:       addr,
		managementKey: strings.TrimSpace(opts.ManagementKey),
		timeout:       opts.Timeout,
		queueKey:      strings.TrimSpace(opts.QueueKey),
		batchSize:     opts.BatchSize,
		httpClient:    NewClient(opts.BaseURL, opts.ManagementKey, opts.Timeout, opts.TLSSkipVerify),
		dial:          dial,
	}
}

func (c *RedisQueueClient) PopUsage(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("redis queue client is nil")
	}
	if c.queueKey == "" {
		return nil, fmt.Errorf("redis queue key is required")
	}
	if c.batchSize <= 0 {
		return nil, fmt.Errorf("redis queue batch size must be positive")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.syncMode {
	case redisQueueSyncModeRedis:
		messages, err := c.popUsageOverRedis(ctx)
		if err == nil {
			return messages, nil
		}
		return c.popUsageOverHTTPFallback(ctx, err)
	case redisQueueSyncModeHTTP:
		return c.popUsageOverHTTP(ctx)
	}

	messages, err := c.popUsageOverRedis(ctx)
	if err == nil {
		c.syncMode = redisQueueSyncModeRedis
		logrus.WithField("message_count", len(messages)).Info("usage queue sync used redis protocol")
		return messages, nil
	}
	return c.popUsageOverHTTPFallback(ctx, err)
}

func (c *RedisQueueClient) popUsageOverHTTPFallback(ctx context.Context, redisErr error) ([]string, error) {
	logrus.WithField("redis_error", redisErr.Error()).Error("usage queue sync failed to used redis protocol")
	if !c.canFallbackToHTTP() {
		return nil, fmt.Errorf("usage queue sync failed: %w; http usage queue fallback not possible", redisErr)
	}

	messages, fallbackErr := c.popUsageOverHTTP(ctx)
	if fallbackErr != nil {
		return nil, fmt.Errorf("usage queue sync failed: %w; http usage queue fallback failed: %w", redisErr, fallbackErr)
	}
	c.syncMode = redisQueueSyncModeHTTP
	logrus.WithField("message_count", len(messages)).Info("usage queue sync used http protocol")
	return messages, nil
}

func (c *RedisQueueClient) popUsageOverRedis(ctx context.Context) ([]string, error) {
	conn, reader, err := c.openAuthenticatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := writeRESPCommand(conn, cpaManagementRedisPopCommand, c.queueKey, strconv.Itoa(c.batchSize)); err != nil {
		return nil, fmt.Errorf("write redis queue pop command: %w", err)
	}
	popResponse, err := readRESPValue(reader)
	if err != nil {
		return nil, fmt.Errorf("read redis queue pop response: %w", err)
	}
	if popResponse.err != "" {
		return nil, fmt.Errorf("redis queue pop failed: %s", popResponse.err)
	}
	return popResponse.strings(), nil
}

func (c *RedisQueueClient) canFallbackToHTTP() bool {
	return c != nil && c.httpClient != nil && strings.TrimSpace(c.httpClient.baseURL) != ""
}

func (c *RedisQueueClient) popUsageOverHTTP(ctx context.Context) ([]string, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("redis queue http client is nil")
	}
	result, err := c.httpClient.FetchUsageQueue(ctx, c.batchSize)
	if err != nil {
		return nil, fmt.Errorf("fetch usage queue over http: %w", err)
	}
	messages := make([]string, 0, len(result.Payload))
	for _, item := range result.Payload {
		trimmed := strings.TrimSpace(string(item))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		messages = append(messages, trimmed)
	}
	return messages, nil
}

func (c *RedisQueueClient) openAuthenticatedConnection(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("redis queue client is nil")
	}
	if c.address == "" {
		return nil, nil, fmt.Errorf("redis queue address is required")
	}
	if c.managementKey == "" {
		return nil, nil, fmt.Errorf("redis queue management key is required")
	}

	conn, err := c.dial(ctx, cpaManagementRedisNetwork, c.address)
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis queue: %w", err)
	}
	if c.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.timeout))
	}

	reader := bufio.NewReader(conn)
	if err := writeRESPCommand(conn, cpaManagementRedisAuthCommand, c.managementKey); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("write redis queue auth command: %w", err)
	}
	authResponse, err := readRESPValue(reader)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read redis queue auth response: %w", err)
	}
	if authResponse.err != "" {
		conn.Close()
		return nil, nil, fmt.Errorf("%w: %s", ErrRedisQueueAuth, authResponse.err)
	}
	return conn, reader, nil
}

func redisQueueAddress(baseURL, redisQueueAddr string) (string, bool) {
	override := strings.TrimSpace(redisQueueAddr)
	if override != "" {
		if parsed, err := url.Parse(override); err == nil && parsed.Host != "" {
			return parsed.Host, parsed.Scheme == "rediss"
		}
		return override, false
	}
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		useTLS := parsed.Scheme == "https"
		if parsed.Port() != "" {
			return parsed.Host, useTLS
		}
		return net.JoinHostPort(parsed.Hostname(), ManagementRedisDefaultPort), useTLS
	}
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "http://"), "https://")
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return trimmed, false
	}
	return net.JoinHostPort(trimmed, ManagementRedisDefaultPort), false
}

func writeRESPCommand(writer io.Writer, parts ...string) error {
	if _, err := fmt.Fprintf(writer, "*%d\r\n", len(parts)); err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(part), part); err != nil {
			return err
		}
	}
	return nil
}

type respValue struct {
	simple string
	bulk   *string
	array  []respValue
	err    string
	nil    bool
}

func (v respValue) strings() []string {
	if v.nil {
		return nil
	}
	if v.bulk != nil {
		return []string{*v.bulk}
	}
	if len(v.array) == 0 {
		return nil
	}
	items := make([]string, 0, len(v.array))
	for _, item := range v.array {
		if item.bulk != nil {
			items = append(items, *item.bulk)
		}
	}
	return items
}

func readRESPValue(reader *bufio.Reader) (respValue, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return respValue{}, err
	}
	switch prefix {
	case '+':
		line, err := readRESPLine(reader)
		return respValue{simple: line}, err
	case '-':
		line, err := readRESPLine(reader)
		return respValue{err: line}, err
	case '$':
		return readRESPBulk(reader)
	case '*':
		return readRESPArray(reader)
	default:
		return respValue{}, fmt.Errorf("unexpected RESP prefix %q", prefix)
	}
}

func readRESPBulk(reader *bufio.Reader) (respValue, error) {
	line, err := readRESPLine(reader)
	if err != nil {
		return respValue{}, err
	}
	size, err := strconv.Atoi(line)
	if err != nil {
		return respValue{}, fmt.Errorf("parse bulk size: %w", err)
	}
	if size < 0 {
		return respValue{nil: true}, nil
	}
	buf := make([]byte, size+2)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return respValue{}, err
	}
	value := string(buf[:size])
	return respValue{bulk: &value}, nil
}

func readRESPArray(reader *bufio.Reader) (respValue, error) {
	line, err := readRESPLine(reader)
	if err != nil {
		return respValue{}, err
	}
	count, err := strconv.Atoi(line)
	if err != nil {
		return respValue{}, fmt.Errorf("parse array size: %w", err)
	}
	if count < 0 {
		return respValue{nil: true}, nil
	}
	items := make([]respValue, 0, count)
	for range count {
		item, err := readRESPValue(reader)
		if err != nil {
			return respValue{}, err
		}
		items = append(items, item)
	}
	return respValue{array: items}, nil
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
