package cpa

import (
	"bufio"
	"context"
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

type RedisQueueClient struct {
	address       string
	managementKey string
	timeout       time.Duration
	queueKey      string
	batchSize     int
}

func NewRedisQueueClient(baseURL, redisQueueAddr, managementKey string, timeout time.Duration, queueKey string, batchSize int) *RedisQueueClient {
	return &RedisQueueClient{
		address:       redisQueueAddress(baseURL, redisQueueAddr),
		managementKey: strings.TrimSpace(managementKey),
		timeout:       timeout,
		queueKey:      strings.TrimSpace(queueKey),
		batchSize:     batchSize,
	}
}

func (c *RedisQueueClient) PopUsage(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("redis queue client is nil")
	}
	if c.address == "" {
		return nil, fmt.Errorf("redis queue address is required")
	}
	if c.managementKey == "" {
		return nil, fmt.Errorf("redis queue management key is required")
	}
	if c.queueKey == "" {
		return nil, fmt.Errorf("redis queue key is required")
	}
	if c.batchSize <= 0 {
		return nil, fmt.Errorf("redis queue batch size must be positive")
	}

	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("connect redis queue: %w", err)
	}
	defer conn.Close()
	if c.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.timeout))
	}

	reader := bufio.NewReader(conn)
	if err := writeRESPCommand(conn, "AUTH", c.managementKey); err != nil {
		return nil, fmt.Errorf("write redis queue auth command: %w", err)
	}
	authResponse, err := readRESPValue(reader)
	if err != nil {
		return nil, fmt.Errorf("read redis queue auth response: %w", err)
	}
	if authResponse.err != "" {
		return nil, fmt.Errorf("%w: %s", ErrRedisQueueAuth, authResponse.err)
	}

	if err := writeRESPCommand(conn, "LPOP", c.queueKey, strconv.Itoa(c.batchSize)); err != nil {
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

func redisQueueAddress(baseURL, redisQueueAddr string) string {
	override := strings.TrimSpace(redisQueueAddr)
	if override != "" {
		if parsed, err := url.Parse(override); err == nil && parsed.Host != "" {
			return parsed.Host
		}
		return override
	}
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		if parsed.Port() != "" {
			return parsed.Host
		}
		return net.JoinHostPort(parsed.Hostname(), "8317")
	}
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "http://"), "https://")
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return trimmed
	}
	return net.JoinHostPort(trimmed, "8317")
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
