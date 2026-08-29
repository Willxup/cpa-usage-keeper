package poller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
)

const redisErrorEventLogInterval = time.Minute

var redisErrorDefaultRetryDelays = []time.Duration{
	10 * time.Second,
	30 * time.Second,
	time.Minute,
	3 * time.Minute,
	10 * time.Minute,
}

// DefaultRedisErrorRetryDelays 返回副本，避免调用方修改全局有限重试策略。
func DefaultRedisErrorRetryDelays() []time.Duration {
	return append([]time.Duration(nil), redisErrorDefaultRetryDelays...)
}

// ErrorEventSink 接收原始 CPA JSON，并负责 typed decode 和直接写最终表。
type ErrorEventSink interface {
	StoreErrorEvent(context.Context, string, time.Time) error
}

// RedisErrorIngestRunnerOptions 只开放测试所需的时间依赖；生产使用固定五次重试策略。
type RedisErrorIngestRunnerOptions struct {
	// RetryDelays 是首次失败后的有限等待序列；nil 使用生产固定五档间隔，空切片表示不重试。
	RetryDelays []time.Duration
	// Now 生成 received_at 并控制事件失败日志限频；nil 使用 time.Now。
	Now func() time.Time
	// Sleep 等待下一次订阅重试并响应 context 取消；nil 使用 timer 实现。
	Sleep func(context.Context, time.Duration) bool
}

// RedisErrorIngestRunner 独立管理 CPA errors 订阅；任何失败都只停用本 runner，不向 App 传播。
type RedisErrorIngestRunner struct {
	source      ErrorSubscriptionSource
	sink        ErrorEventSink
	retryDelays []time.Duration
	now         func() time.Time
	sleep       func(context.Context, time.Duration) bool

	lastEventErrorLogAt time.Time
}

// NewRedisErrorIngestRunner 使用产品约定的五次有限重试策略创建 runner。
func NewRedisErrorIngestRunner(source ErrorSubscriptionSource, sink ErrorEventSink) *RedisErrorIngestRunner {
	return NewRedisErrorIngestRunnerWithOptions(source, sink, RedisErrorIngestRunnerOptions{})
}

// NewRedisErrorIngestRunnerWithOptions 允许测试压缩等待时间，不改变生产默认间隔。
func NewRedisErrorIngestRunnerWithOptions(source ErrorSubscriptionSource, sink ErrorEventSink, options RedisErrorIngestRunnerOptions) *RedisErrorIngestRunner {
	retryDelays := options.RetryDelays
	if retryDelays == nil {
		retryDelays = DefaultRedisErrorRetryDelays()
	} else {
		retryDelays = append([]time.Duration(nil), retryDelays...)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = redisErrorSleepContext
	}
	return &RedisErrorIngestRunner{source: source, sink: sink, retryDelays: retryDelays, now: now, sleep: sleep}
}

// Run 持续接收直至 context 取消或有限重试耗尽；Errors 是可选能力，因此始终返回 nil。
func (r *RedisErrorIngestRunner) Run(ctx context.Context) error {
	if r == nil || r.source == nil || r.sink == nil {
		logrus.Error("CPA errors ingest disabled because dependencies are unavailable")
		return nil
	}
	logrus.Info("CPA errors ingest started")
	retryIndex := 0
	for {
		subscription, err := r.source.SubscribeErrors(ctx)
		if err == nil && subscription == nil {
			err = fmt.Errorf("CPA errors subscription is nil")
		}
		if err == nil {
			// 成功订阅即清零上一轮失败计数；未来断线重新获得完整五次重试机会。
			retryIndex = 0
			err = r.receive(ctx, subscription)
			_ = subscription.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		if redisErrorPermanentSubscribeFailure(err) {
			logrus.WithError(err).Warn("CPA errors ingest disabled for this process")
			return nil
		}
		if retryIndex >= len(r.retryDelays) {
			logrus.WithError(err).Warn("CPA errors ingest stopped after retry limit")
			return nil
		}

		delay := r.retryDelays[retryIndex]
		retryIndex++
		// 只记录连接级失败和下一次尝试，不记录任何 Error Event payload 或身份字段。
		logrus.WithFields(logrus.Fields{"retry": retryIndex, "retry_in": delay}).WithError(err).Warn("CPA errors subscription unavailable")
		if !r.sleep(ctx, delay) {
			return nil
		}
	}
}

func (r *RedisErrorIngestRunner) receive(ctx context.Context, subscription ErrorSubscription) error {
	for {
		payload, err := subscription.Receive(ctx)
		if err != nil {
			return err
		}
		receivedAt := timeutil.NormalizeStorageTime(r.now())
		if err := r.sink.StoreErrorEvent(ctx, payload, receivedAt); err != nil {
			// 无 Inbox 意味着本条写入失败后无法补偿；丢弃并继续读，避免 Errors 反向阻断订阅或主程序。
			r.logEventFailure(err)
		}
	}
}

func (r *RedisErrorIngestRunner) logEventFailure(err error) {
	now := r.now()
	if !r.lastEventErrorLogAt.IsZero() && now.Sub(r.lastEventErrorLogAt) < redisErrorEventLogInterval {
		return
	}
	r.lastEventErrorLogAt = now
	logrus.WithError(err).Warn("CPA error event was dropped")
}

func redisErrorPermanentSubscribeFailure(err error) bool {
	return errors.Is(err, ErrRedisErrorSubscribeUnsupported) || errors.Is(err, cpa.ErrRedisQueueAuth)
}

func redisErrorSleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
