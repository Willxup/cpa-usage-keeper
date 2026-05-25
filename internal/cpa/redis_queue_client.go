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
	"time"
)

var ErrRedisQueueAuth = errors.New("redis queue auth failed")

const redisQueueMaxRESPBulkSize = 4 * 1024 * 1024
const redisQueueMaxRESPArrayLength = ManagementUsageQueueMaxBatchSize
const redisQueueMaxRESPTotalBulkSize = 16 * 1024 * 1024
const redisQueueMaxRESPLineLength = 4096
const redisQueueMaxRESPDepth = 4

type RedisQueueClient struct {
	address       string
	managementKey string
	timeout       time.Duration
	queueKey      string
	batchSize     int
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
	return c.popUsageOverRedis(ctx)
}

func (c *RedisQueueClient) popUsageOverRedis(ctx context.Context) ([]string, error) {
	conn, reader, err := c.openAuthenticatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := writeRESPCommand(conn, ManagementRedisPopCommand, c.queueKey, strconv.Itoa(c.batchSize)); err != nil {
		return nil, fmt.Errorf("write redis queue pop command: %w", err)
	}
	popResponse, err := readRESPValueWithLimits(reader, c.batchSize)
	if err != nil {
		return nil, fmt.Errorf("read redis queue pop response: %w", err)
	}
	if popResponse.err != "" {
		return nil, fmt.Errorf("redis queue pop failed: %s", popResponse.err)
	}
	return popResponse.strings(), nil
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
	if err := writeRESPCommand(conn, ManagementRedisAuthCommand, c.managementKey); err != nil {
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
	return readRESPValueWithLimits(reader, redisQueueMaxRESPArrayLength)
}

func readRESPValueWithLimits(reader *bufio.Reader, maxArrayLength int) (respValue, error) {
	if maxArrayLength <= 0 || maxArrayLength > redisQueueMaxRESPArrayLength {
		maxArrayLength = redisQueueMaxRESPArrayLength
	}
	remainingBulkBytes := redisQueueMaxRESPTotalBulkSize
	return readRESPValueLimited(reader, maxArrayLength, &remainingBulkBytes, 0)
}

func readRESPValueLimited(reader *bufio.Reader, maxArrayLength int, remainingBulkBytes *int, depth int) (respValue, error) {
	if depth > redisQueueMaxRESPDepth {
		return respValue{}, fmt.Errorf("redis queue response nesting exceeds maximum depth")
	}
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
		return readRESPBulk(reader, remainingBulkBytes)
	case '*':
		return readRESPArray(reader, maxArrayLength, remainingBulkBytes, depth)
	default:
		return respValue{}, fmt.Errorf("unexpected RESP prefix %q", prefix)
	}
}

func readRESPBulk(reader *bufio.Reader, remainingBulkBytes *int) (respValue, error) {
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
	if size > redisQueueMaxRESPBulkSize {
		return respValue{}, fmt.Errorf("redis queue bulk string exceeds maximum size")
	}
	if remainingBulkBytes != nil {
		if size > *remainingBulkBytes {
			return respValue{}, fmt.Errorf("redis queue response exceeds maximum total bulk size")
		}
		*remainingBulkBytes -= size
	}
	buf := make([]byte, size+2)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return respValue{}, err
	}
	if string(buf[size:]) != "\r\n" {
		return respValue{}, fmt.Errorf("malformed redis queue bulk string")
	}
	value := string(buf[:size])
	return respValue{bulk: &value}, nil
}

func readRESPArray(reader *bufio.Reader, maxArrayLength int, remainingBulkBytes *int, depth int) (respValue, error) {
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
	if count > maxArrayLength {
		return respValue{}, fmt.Errorf("redis queue array exceeds maximum length")
	}
	items := make([]respValue, 0, count)
	for range count {
		item, err := readRESPValueLimited(reader, maxArrayLength, remainingBulkBytes, depth+1)
		if err != nil {
			return respValue{}, err
		}
		items = append(items, item)
	}
	return respValue{array: items}, nil
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	line := make([]byte, 0, 128)
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > redisQueueMaxRESPLineLength {
			return "", fmt.Errorf("redis queue line exceeds maximum length")
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return "", err
	}
	if !strings.HasSuffix(string(line), "\r\n") {
		return "", fmt.Errorf("malformed redis queue line")
	}
	return strings.TrimSuffix(string(line), "\r\n"), nil
}
