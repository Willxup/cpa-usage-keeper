package poller

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

// ErrRedisErrorSubscribeUnsupported 表示 CPA 明确不支持 errors channel；重试无法改变该进程的上游能力。
var ErrRedisErrorSubscribeUnsupported = errors.New("CPA errors subscription is unsupported")

// ErrorSubscription 是 Errors runner 需要的最小长连接接口，与现有 UsageSubscription 完全隔离。
type ErrorSubscription interface {
	Receive(context.Context) (string, error)
	Close() error
}

// ErrorSubscriptionSource 只负责建立固定 errors channel 订阅，不承担重试或持久化。
type ErrorSubscriptionSource interface {
	SubscribeErrors(context.Context) (ErrorSubscription, error)
}

// RedisErrorSubscribeSource 保存 Errors 专用 RESP 连接配置；构造阶段不主动联网。
//
// 该类型故意不参数化或改写现有 RedisSubscribeSource，确保 Usage 订阅行为不受 Errors 功能影响。
type RedisErrorSubscribeSource struct {
	address       string
	managementKey string
	timeout       time.Duration
	dial          func(context.Context, string, string) (net.Conn, error)
}

// NewRedisErrorSubscribeSource 创建固定订阅 errors channel 的独立 source。
func NewRedisErrorSubscribeSource(opts RedisSubscribeOptions) *RedisErrorSubscribeSource {
	// 地址/TLS 推导保持与 Keeper 其它 CPA RESP 客户端一致，但 Errors 拥有自己的连接和生命周期。
	addr, useTLS := redisIngestAddress(opts.BaseURL, opts.RedisAddr)
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
	return &RedisErrorSubscribeSource{
		address:       addr,
		managementKey: strings.TrimSpace(opts.ManagementKey),
		timeout:       opts.Timeout,
		dial:          dial,
	}
}

// SubscribeErrors 建立新连接，完成 AUTH 和固定 errors SUBSCRIBE 握手后返回长期订阅。
func (s *RedisErrorSubscribeSource) SubscribeErrors(ctx context.Context) (ErrorSubscription, error) {
	if s == nil {
		return nil, fmt.Errorf("redis error subscribe source is nil")
	}
	if s.address == "" {
		return nil, fmt.Errorf("redis error subscribe address is required")
	}
	if s.managementKey == "" {
		return nil, fmt.Errorf("redis error subscribe management key is required")
	}
	conn, err := s.dial(ctx, "tcp", s.address)
	if err != nil {
		return nil, fmt.Errorf("connect redis errors subscription: %w", err)
	}
	// DialContext 只覆盖建连；连接成功后的 AUTH/SUBSCRIBE read 也必须响应 App 取消。
	stopHandshakeCancelWatch := watchRedisErrorHandshakeCancellation(ctx, conn)
	defer stopHandshakeCancelWatch()
	if s.timeout > 0 {
		// deadline 只限制连接、AUTH 和 SUBSCRIBE 握手；成功后必须清除，允许长期阻塞接收。
		_ = conn.SetDeadline(time.Now().Add(s.timeout))
	}
	reader := bufio.NewReader(conn)
	if err := writeRedisIngestRESPCommand(conn, cpa.ManagementRedisAuthCommand, s.managementKey); err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("write redis errors auth command: %w", err)
	}
	authResponse, err := readRedisIngestRESPValue(reader)
	if err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("read redis errors auth response: %w", err)
	}
	if authResponse.err != "" {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %s", cpa.ErrRedisQueueAuth, authResponse.err)
	}
	if err := writeRedisIngestRESPCommand(conn, cpa.ManagementRedisSubscribeCommand, cpa.ManagementErrorsSubscribeChannel); err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("write redis errors subscribe command: %w", err)
	}
	subscribeResponse, err := readRedisIngestRESPValue(reader)
	if err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("read redis errors subscribe response: %w", err)
	}
	if subscribeResponse.err != "" {
		_ = conn.Close()
		if strings.Contains(strings.ToLower(subscribeResponse.err), "unsupported channel") {
			return nil, fmt.Errorf("%w: %s", ErrRedisErrorSubscribeUnsupported, subscribeResponse.err)
		}
		return nil, fmt.Errorf("redis errors subscribe failed: %s", subscribeResponse.err)
	}
	if !redisErrorSubscribeAck(subscribeResponse) {
		_ = conn.Close()
		return nil, fmt.Errorf("redis errors subscribe returned unexpected response")
	}
	// 握手结束后先停掉 watcher，再把连接交给长期 subscription，避免 watcher 竞态关闭已转交连接。
	stopHandshakeCancelWatch()
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = conn.Close()
		return nil, ctxErr
	}
	_ = conn.SetDeadline(time.Time{})
	return &redisErrorSubscription{conn: conn, reader: reader}, nil
}

// watchRedisErrorHandshakeCancellation 在专用握手阶段把 context 取消转换成 conn.Close，唤醒阻塞 I/O。
func watchRedisErrorHandshakeCancellation(ctx context.Context, conn net.Conn) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

type redisErrorSubscription struct {
	conn   net.Conn
	reader *bufio.Reader
}

// Receive 阻塞等待下一条 errors message；取消 context 时关闭专用连接以唤醒底层 read。
func (s *redisErrorSubscription) Receive(ctx context.Context) (string, error) {
	if s == nil || s.conn == nil || s.reader == nil {
		return "", fmt.Errorf("redis errors subscription is nil")
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_ = s.conn.SetReadDeadline(time.Time{})
		cancelWatchDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = s.conn.Close()
			case <-cancelWatchDone:
			}
		}()
		value, err := readRedisIngestRESPValue(s.reader)
		close(cancelWatchDone)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			return "", err
		}
		if value.err != "" {
			return "", fmt.Errorf("redis errors subscription error: %s", value.err)
		}
		if payload, ok := redisErrorSubscriptionMessage(value); ok {
			return payload, nil
		}
	}
}

func (s *redisErrorSubscription) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func redisErrorSubscribeAck(value redisIngestRESPValue) bool {
	if len(value.array) < 3 {
		return false
	}
	return strings.EqualFold(value.array[0].stringValue(), "subscribe") && value.array[1].stringValue() == cpa.ManagementErrorsSubscribeChannel
}

func redisErrorSubscriptionMessage(value redisIngestRESPValue) (string, bool) {
	if len(value.array) < 3 || !strings.EqualFold(value.array[0].stringValue(), "message") || value.array[1].stringValue() != cpa.ManagementErrorsSubscribeChannel {
		return "", false
	}
	payload := value.array[2].stringValue()
	if strings.TrimSpace(payload) == "" {
		return "", false
	}
	return payload, true
}
