package poller

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

	"cpa-usage-keeper/internal/cpa"
)

// 单条 usage JSON 不应该接近这个大小；上限用于防止异常 RESP bulk 触发大内存分配。
const redisIngestMaxRESPBulkSize = 4 * 1024 * 1024

// Redis 订阅 ack/message 都是很短的数组；限制数组长度避免异常响应无限占用内存。
const redisIngestMaxRESPArrayLength = 16

// RESP simple/error/integer 行也需要上限，避免异常 peer 长行占用内存。
const redisIngestMaxRESPLineLength = 4096

// 单个订阅事件的 bulk 总量不应超过单条 payload 上限加少量协议字段。
const redisIngestMaxRESPTotalBulkSize = redisIngestMaxRESPBulkSize + 4096

// Redis 订阅事件不需要深层嵌套；限制深度避免异常响应耗尽栈。
const redisIngestMaxRESPDepth = 4

type RedisSubscribeOptions struct {
	BaseURL       string
	RedisAddr     string
	ManagementKey string
	Timeout       time.Duration
	TLS           bool
	TLSSkipVerify bool
}

type RedisSubscribeSource struct {
	// address 是最终 Redis TCP 地址，优先使用 RedisAddr，缺省从 CPA BaseURL 推导。
	address string
	// managementKey 用于 Redis AUTH，来源是 CPA_MANAGEMENT_KEY。
	managementKey string
	// timeout 限制连接、AUTH、SUBSCRIBE 握手耗时，不限制后续长期阻塞接收。
	timeout time.Duration
	// dial 抽象出来便于 TLS/非 TLS 统一创建连接，也便于测试替换。
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func NewRedisSubscribeSource(opts RedisSubscribeOptions) *RedisSubscribeSource {
	// 先根据显式 Redis 地址或 CPA BaseURL 推导地址和 TLS 默认值。
	addr, useTLS := redisIngestAddress(opts.BaseURL, opts.RedisAddr)
	if opts.TLS {
		// 显式 RedisQueueTLS 配置优先级高于 URL scheme 推导。
		useTLS = true
	}
	// net.Dialer 负责普通 TCP 连接和基础 timeout。
	netDialer := &net.Dialer{Timeout: opts.Timeout}
	// 默认使用非 TLS dial。
	dial := netDialer.DialContext
	if useTLS {
		// TLS 配置沿用 CPA/Redis 队列配置；是否跳过证书验证由运行配置控制。
		tlsDialer := &tls.Dialer{NetDialer: netDialer, Config: &tls.Config{InsecureSkipVerify: opts.TLSSkipVerify}}
		// TLS DialContext 不总是完全尊重 net.Dialer.Timeout，所以这里再包一层 deadline。
		dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if opts.Timeout > 0 {
				// 为本次 dial 计算一个不晚于配置 timeout 的 deadline。
				deadline := time.Now().Add(opts.Timeout)
				if existing, ok := ctx.Deadline(); !ok || deadline.Before(existing) {
					// 只有当当前 context 没有更早 deadline 时才收紧 deadline。
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, deadline)
					defer cancel()
				}
			}
			// 真正发起 TLS 连接。
			return tlsDialer.DialContext(ctx, network, addr)
		}
	}
	// 构造 source 时只保存配置，不主动联网。
	return &RedisSubscribeSource{
		// address 在 Subscribe 时校验，允许构造阶段不报错。
		address: addr,
		// managementKey 只 trim 空白，不写入日志，避免泄漏密钥。
		managementKey: strings.TrimSpace(opts.ManagementKey),
		// timeout 后续用于 AUTH/SUBSCRIBE 握手 deadline。
		timeout: opts.Timeout,
		// dial 保存最终 TCP/TLS 连接函数。
		dial: dial,
	}
}

func (s *RedisSubscribeSource) Subscribe(ctx context.Context) (UsageSubscription, error) {
	if s == nil {
		// nil source 是构造错误，直接返回。
		return nil, fmt.Errorf("redis subscribe source is nil")
	}
	if s.address == "" {
		// 没有地址时无法建立 Redis TCP 连接。
		return nil, fmt.Errorf("redis subscribe address is required")
	}
	if s.managementKey == "" {
		// Redis AUTH 必须使用 CPA management key。
		return nil, fmt.Errorf("redis subscribe management key is required")
	}
	// 建立 TCP/TLS 连接；这里还没有发送任何 Redis 命令。
	conn, err := s.dial(ctx, "tcp", s.address)
	if err != nil {
		return nil, fmt.Errorf("connect redis subscribe: %w", err)
	}
	if s.timeout > 0 {
		// 握手阶段设置总 deadline，避免 AUTH/SUBSCRIBE 卡住启动探测。
		_ = conn.SetDeadline(time.Now().Add(s.timeout))
	}
	// RESP 读取需要缓冲 reader，后续 subscription 也复用同一个 reader。
	reader := bufio.NewReader(conn)
	// Redis subscribe 连接必须先 AUTH，密码就是 CPA_MANAGEMENT_KEY。
	if err := writeRedisIngestRESPCommand(conn, cpa.ManagementRedisAuthCommand, s.managementKey); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write redis subscribe auth command: %w", err)
	}
	// 读取 AUTH 响应，不能只写不读，否则后续 SUBSCRIBE ack 会错位。
	authResponse, err := readRedisIngestRESPValue(reader)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read redis subscribe auth response: %w", err)
	}
	if authResponse.err != "" {
		// Redis 返回 -ERR 时关闭连接并映射为已有 auth 错误语义。
		conn.Close()
		return nil, fmt.Errorf("%w: %s", cpa.ErrRedisQueueAuth, authResponse.err)
	}
	// AUTH 成功后订阅 usage channel。
	if err := writeRedisIngestRESPCommand(conn, cpa.ManagementRedisSubscribeCommand, cpa.ManagementUsageSubscribeChannel); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write redis subscribe command: %w", err)
	}
	// 必须读取并验证 SUBSCRIBE ack，不能把“命令写入成功”当作“订阅成功”。
	subscribeResponse, err := readRedisIngestRESPValue(reader)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read redis subscribe response: %w", err)
	}
	if subscribeResponse.err != "" {
		// Redis 明确拒绝订阅时关闭连接，交给 runner 降级。
		conn.Close()
		return nil, fmt.Errorf("redis subscribe failed: %s", subscribeResponse.err)
	}
	if !redisIngestSubscribeAck(subscribeResponse) {
		// 非预期 ack 说明协议状态不可信，不能进入 subscribe 模式。
		conn.Close()
		return nil, fmt.Errorf("redis subscribe returned unexpected response")
	}
	// 握手完成后清掉 deadline，让订阅接收可以长期低 CPU 阻塞等待推送。
	_ = conn.SetDeadline(time.Time{})
	// 返回长期 subscription，runner 负责 Close 生命周期。
	return &redisUsageSubscription{conn: conn, reader: reader}, nil
}

type redisUsageSubscription struct {
	// conn 用于设置 read deadline 和取消时关闭阻塞读。
	conn net.Conn
	// reader 保存 RESP 解析状态，不能每次 Receive 重建。
	reader *bufio.Reader
}

func (s *redisUsageSubscription) Receive(ctx context.Context) (string, error) {
	if s == nil || s.reader == nil || s.conn == nil {
		// subscription 不完整时不能继续读取。
		return "", fmt.Errorf("redis subscription is nil")
	}
	// 循环是为了跳过 subscribe ack、空消息或非 usage channel 消息。
	for {
		if err := ctx.Err(); err != nil {
			// 调用方取消时立即返回，不再阻塞网络读。
			return "", err
		}
		// cancelWatchDone 用于停止取消监听 goroutine。
		cancelWatchDone := make(chan struct{})
		if ctxDeadline, ok := ctx.Deadline(); ok {
			// 有 deadline 的场景通常是 1s batch window，直接映射为 socket read deadline。
			_ = s.conn.SetReadDeadline(ctxDeadline)
		} else {
			// 无 deadline 的订阅首条消息读取应无限阻塞，避免 1s 轮询消耗 CPU。
			_ = s.conn.SetReadDeadline(time.Time{})
		}
		go func() {
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.Canceled) {
					// 即使 context 有较晚 deadline，提前取消也要关闭连接唤醒阻塞读。
					_ = s.conn.Close()
				}
			case <-cancelWatchDone:
				// 正常读到消息后退出 goroutine，避免泄漏。
			}
		}()
		// 从 Redis 连接读取一个完整 RESP value。
		value, err := readRedisIngestRESPValue(s.reader)
		// 本次 read 已结束，通知取消监听 goroutine 退出。
		close(cancelWatchDone)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				// 如果错误由 context 取消触发，优先返回 context 错误给 runner。
				return "", ctxErr
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// read deadline 超时但 context 还没标记完成时，继续循环再检查一次。
				continue
			}
			// 其他网络/协议错误代表订阅断开。
			return "", err
		}
		if value.err != "" {
			// Redis 主动返回错误时终止订阅，让 runner 进入降级。
			return "", fmt.Errorf("redis subscription error: %s", value.err)
		}
		// 只接受 message usage payload，其他 RESP 事件忽略。
		message, ok := redisIngestSubscriptionMessage(value)
		if ok {
			// 返回原始 usage JSON 字符串，不做 decode。
			return message, nil
		}
	}
}

func (s *redisUsageSubscription) Close() error {
	if s == nil || s.conn == nil {
		// nil Close 保持幂等，方便 defer 调用。
		return nil
	}
	// 关闭底层连接会唤醒任何正在阻塞的 Receive。
	return s.conn.Close()
}

func redisIngestSubscribeAck(value redisIngestRESPValue) bool {
	if len(value.array) < 3 {
		// Redis SUBSCRIBE ack 至少包含 kind、channel、订阅数量。
		return false
	}
	// 第 1 项必须是 subscribe。
	kind := strings.ToLower(value.array[0].stringValue())
	// 第 2 项必须是 usage channel。
	channel := value.array[1].stringValue()
	// 只验证我们关心的 channel，订阅数量不影响后续逻辑。
	return kind == "subscribe" && channel == cpa.ManagementUsageSubscribeChannel
}

func redisIngestSubscriptionMessage(value redisIngestRESPValue) (string, bool) {
	if len(value.array) < 3 {
		// message 事件至少包含 kind、channel、payload。
		return "", false
	}
	// 第 1 项必须是 Redis message 事件。
	kind := strings.ToLower(value.array[0].stringValue())
	// 第 2 项必须是 usage channel。
	channel := value.array[1].stringValue()
	if kind != "message" || channel != cpa.ManagementUsageSubscribeChannel {
		// 其他 channel 或事件类型直接忽略。
		return "", false
	}
	// 第 3 项是 CPA 推送的 raw usage JSON。
	payload := value.array[2].stringValue()
	if strings.TrimSpace(payload) == "" {
		// 空 payload 不写入 inbox，避免无意义 decode 失败。
		return "", false
	}
	// 返回未解码的 payload，保持和 Redis/HTTP queue 旧路径一致。
	return payload, true
}

func redisIngestAddress(baseURL, redisAddr string) (string, bool) {
	// 显式 Redis 地址优先级最高。
	override := strings.TrimSpace(redisAddr)
	if override != "" {
		// 支持 rediss://host:port 这种带 scheme 的配置。
		if parsed, err := url.Parse(override); err == nil && parsed.Host != "" {
			return parsed.Host, parsed.Scheme == "rediss"
		}
		// 不带 scheme 时按 host:port 原样使用，TLS 由显式配置决定。
		return override, false
	}
	// Redis 地址缺省时从 CPA base URL 推导。
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		// 没有 CPA base URL 时无法推导地址。
		return "", false
	}
	// 优先按标准 URL 解析。
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		// https CPA 默认对应 TLS Redis，http 默认非 TLS。
		useTLS := parsed.Scheme == "https"
		if parsed.Port() != "" {
			// base URL 明确写了端口时沿用 host:port。
			return parsed.Host, useTLS
		}
		// base URL 没端口时使用 CPA 管理 Redis 默认端口。
		return net.JoinHostPort(parsed.Hostname(), cpa.ManagementRedisDefaultPort), useTLS
	}
	// 兼容不规范 URL 字符串配置，先去掉常见 scheme 前缀。
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "http://"), "https://")
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		// 已包含端口时直接使用。
		return trimmed, false
	}
	// 最后用默认 Redis 管理端口补齐地址。
	return net.JoinHostPort(trimmed, cpa.ManagementRedisDefaultPort), false
}

func writeRedisIngestRESPCommand(writer io.Writer, parts ...string) error {
	// Redis 命令统一写成 RESP array，先写数组长度。
	if _, err := fmt.Fprintf(writer, "*%d\r\n", len(parts)); err != nil {
		return err
	}
	// 每个命令参数都写成 bulk string。
	for _, part := range parts {
		if _, err := fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(part), part); err != nil {
			return err
		}
	}
	return nil
}

type redisIngestRESPValue struct {
	// simple 保存 +OK 或 :1 这类简单字符串/整数响应。
	simple string
	// bulk 保存 $bulk string，nil 表示当前 value 不是 bulk。
	bulk *string
	// array 保存 RESP array 的子项。
	array []redisIngestRESPValue
	// err 保存 Redis -ERR 响应正文。
	err string
	// nil 标记 Redis null bulk/null array。
	nil bool
}

func (v redisIngestRESPValue) stringValue() string {
	if v.bulk != nil {
		// bulk string 是订阅消息 payload/channel 的主要承载形式。
		return *v.bulk
	}
	// 非 bulk 时返回 simple 字符串。
	return v.simple
}

func readRedisIngestRESPValue(reader *bufio.Reader) (redisIngestRESPValue, error) {
	remainingBulkBytes := redisIngestMaxRESPTotalBulkSize
	return readRedisIngestRESPValueLimited(reader, &remainingBulkBytes, 0)
}

func readRedisIngestRESPValueLimited(reader *bufio.Reader, remainingBulkBytes *int, depth int) (redisIngestRESPValue, error) {
	if depth > redisIngestMaxRESPDepth {
		return redisIngestRESPValue{}, fmt.Errorf("redis response nesting exceeds maximum depth")
	}
	// 读取 RESP 前缀决定后续解析方式。
	prefix, err := reader.ReadByte()
	if err != nil {
		return redisIngestRESPValue{}, err
	}
	switch prefix {
	case '+':
		// +simple string 用于 AUTH OK 等响应。
		line, err := readRedisIngestRESPLine(reader)
		return redisIngestRESPValue{simple: line}, err
	case '-':
		// -error string 用于 AUTH/SUBSCRIBE 失败。
		line, err := readRedisIngestRESPLine(reader)
		return redisIngestRESPValue{err: line}, err
	case ':':
		// :integer 在 subscribe ack 的订阅数量中出现，这里转成 simple 使用。
		line, err := readRedisIngestRESPLine(reader)
		return redisIngestRESPValue{simple: line}, err
	case '$':
		// $bulk string 承载 channel 名和 usage payload。
		size, err := readRedisIngestRESPInt(reader)
		if err != nil {
			return redisIngestRESPValue{}, err
		}
		if size < 0 {
			// -1 表示 null bulk。
			return redisIngestRESPValue{nil: true}, nil
		}
		if size > redisIngestMaxRESPBulkSize {
			// 限制异常 bulk 大小，避免恶意或损坏响应分配过多内存。
			return redisIngestRESPValue{}, fmt.Errorf("redis bulk string exceeds maximum size")
		}
		if remainingBulkBytes != nil {
			if size > *remainingBulkBytes {
				return redisIngestRESPValue{}, fmt.Errorf("redis response exceeds maximum total bulk size")
			}
			*remainingBulkBytes -= size
		}
		// 多读 2 字节用于校验 bulk 末尾 CRLF。
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return redisIngestRESPValue{}, err
		}
		if string(buf[size:]) != "\r\n" {
			// RESP bulk 必须以 CRLF 结束，否则协议状态不可信。
			return redisIngestRESPValue{}, fmt.Errorf("malformed redis bulk string")
		}
		// 转成 string 后交给上层按 ack/message 语义解析。
		value := string(buf[:size])
		return redisIngestRESPValue{bulk: &value}, nil
	case '*':
		// *array 用于 subscribe ack 和 message event。
		count, err := readRedisIngestRESPInt(reader)
		if err != nil {
			return redisIngestRESPValue{}, err
		}
		if count < 0 {
			// -1 表示 null array。
			return redisIngestRESPValue{nil: true}, nil
		}
		if count > redisIngestMaxRESPArrayLength {
			// 限制数组长度，避免异常响应递归读取过多元素。
			return redisIngestRESPValue{}, fmt.Errorf("redis array exceeds maximum length")
		}
		// 预分配固定长度以内的数组。
		items := make([]redisIngestRESPValue, 0, count)
		for range count {
			// RESP array 可以嵌套，逐项递归解析。
			item, err := readRedisIngestRESPValueLimited(reader, remainingBulkBytes, depth+1)
			if err != nil {
				return redisIngestRESPValue{}, err
			}
			items = append(items, item)
		}
		return redisIngestRESPValue{array: items}, nil
	default:
		// 不支持的前缀说明协议异常，终止当前订阅。
		return redisIngestRESPValue{}, fmt.Errorf("unsupported redis response prefix %q", prefix)
	}
}

func readRedisIngestRESPLine(reader *bufio.Reader) (string, error) {
	// RESP 行都以 \n 结束，但需要用 ReadSlice 分段读取，避免超长行一次性占用内存。
	line := make([]byte, 0, 128)
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > redisIngestMaxRESPLineLength {
			return "", fmt.Errorf("redis line exceeds maximum length")
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
		// 没有 CRLF 表示协议格式错误。
		return "", fmt.Errorf("malformed redis line")
	}
	// 返回去掉 CRLF 的正文。
	return strings.TrimSuffix(string(line), "\r\n"), nil
}

func readRedisIngestRESPInt(reader *bufio.Reader) (int, error) {
	// RESP 数字先按行读取。
	line, err := readRedisIngestRESPLine(reader)
	if err != nil {
		return 0, err
	}
	// 用 Atoi 避免 fmt.Sscanf 在热点协议解析中产生额外开销。
	value, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("parse redis integer %q: %w", line, err)
	}
	return value, nil
}
