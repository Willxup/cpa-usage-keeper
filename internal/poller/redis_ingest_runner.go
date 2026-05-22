package poller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
)

var errRedisIngestInboxWrite = errors.New("redis ingest inbox write failed")

// Redis ingest 相关文件导览：
// - redis_ingest_runner.go：远端 usage 消息写入 redis_usage_inboxes 的主状态机。
// - redis_ingest_state.go：定义长期 sync mode、临时子状态和状态展示字符串。
// - redis_ingest_backoff.go：定义远端拉取连续失败时的指数退避。
// - redis_ingest_sources.go：定义订阅、拉取和 inbox writer 的接口边界。
// - redis_subscribe_source.go：实现 Redis SUBSCRIBE usage 的连接、认证和消息读取。
// - redis_pull_source.go：实现旧 Redis batch pull，只拉取不 fallback。
// - http_pull_source.go：实现 HTTP usage queue 拉取，只拉取不 fallback。
// - redis_inbox_writer.go：统一把不同来源的 raw usage message 写入 redis_usage_inboxes。
// - redis_process_runner.go：本地 inbox 到 usage_events 的处理 runner，不参与远端拉取模式选择。

type RedisIngestRunnerConfig struct {
	IdleInterval       time.Duration
	BatchSize          int
	HTTPBackoffInitial time.Duration
	HTTPBackoffMax     time.Duration
}

type RedisIngestRunner struct {
	// subscribeSource 只负责建立 Redis SUBSCRIBE usage 连接，不处理 fallback。
	subscribeSource UsageSubscriptionSource
	// redisSource 只负责旧 Redis LPOP 批量拉取，失败原因交给 runner 决策。
	redisSource UsagePullSource
	// httpSource 只负责 HTTP usage queue 拉取，是所有模式的最终降级路径。
	httpSource UsagePullSource
	// writer 是唯一落库出口，保证订阅、Redis pull、HTTP pull 都先进入 redis_usage_inboxes。
	writer RedisInboxWriter
	// config 保存批量大小和空闲等待时间，避免状态机中散落硬编码。
	config RedisIngestRunnerConfig
	// now 允许测试控制时间，特别是恢复探测和退避截断。
	now func() time.Time
	// sleep 允许测试替换等待逻辑，生产环境使用可被 context 打断的 timer。
	sleep func(context.Context, time.Duration) bool
	// failureBackoff 只在连续失败路径递增，任何成功拉取都会 reset。
	failureBackoff *RedisIngestBackoff
	// opMu 串行化远端拉取和 inbox 写入，避免同一 runner 内不同路径重复消费。
	opMu sync.Mutex
	// mu 保护状态字段，Status、日志记录和 runner goroutine 都会访问这些字段。
	mu sync.Mutex
	// running 表示长生命周期 ingest runner 是否在运行，不代表当前是否正在拉取。
	running bool
	// activeOps 统计当前正在执行的 pull/write 操作，保留给状态诊断和并发保护。
	activeOps int
	// mode 是启动探测确定的长期模式：subscribe、redis_pull、http_pull 或 unknown。
	mode RedisIngestSyncMode
	// subState 是长期模式内部的临时阶段，用于日志和状态排查。
	subState RedisIngestSubState
	// lastRunAt 记录最近一次状态或结果更新时间。
	lastRunAt time.Time
	// lastError 保存最近一次真正失败，展示给状态接口。
	lastError string
	// lastWarning 保存最近一次降级/可恢复错误，展示给状态接口。
	lastWarning string
	// lastStatus 保存最近一次状态字符串，兼容现有状态接口展示。
	lastStatus string
}

func NewRedisIngestRunner(sub UsageSubscriptionSource, redis UsagePullSource, http UsagePullSource, writer RedisInboxWriter, cfg RedisIngestRunnerConfig) *RedisIngestRunner {
	// 构造阶段只注入依赖，不主动联网，避免应用启动时阻塞在外部 CPA 服务。
	return &RedisIngestRunner{
		// 订阅源优先级最高，启动探测会先尝试它。
		subscribeSource: sub,
		// Redis pull 源既用于启动探测，也用于 subscribe backfill 和断线降级。
		redisSource: redis,
		// HTTP pull 源是兜底路径，Redis 不可用时依然能继续导入 usage。
		httpSource: http,
		// writer 统一保存 raw message，后续 decode/process 逻辑保持不变。
		writer: writer,
		// config 保留原始配置，validate 会补齐运行所需默认值。
		config: cfg,
		// 默认使用系统时间，测试可以替换为固定时钟。
		now: time.Now,
		// 默认使用 context-aware sleep，保证关停时不会卡住。
		sleep: sleepContext,
		// 失败退避从配置初始值开始，最大值默认 30s。
		failureBackoff: NewRedisIngestBackoff(resolvePositiveDuration(cfg.HTTPBackoffInitial, time.Second), resolvePositiveDuration(cfg.HTTPBackoffMax, 30*time.Second)),
		// 新 runner 尚未完成启动探测，所以先标记 unknown/starting。
		mode:     RedisIngestSyncModeUnknown,
		subState: RedisIngestSubStateStarting,
	}
}

func (r *RedisIngestRunner) Run(ctx context.Context) error {
	// 先校验依赖，避免后台 goroutine 运行后才暴露 nil source/writer。
	if err := r.validate(); err != nil {
		return err
	}
	// Run 生命周期开始，Status.Running 可以反映 ingest 后台任务已启动。
	r.setRunning(true)
	// 无论哪条路径退出，都必须清掉 running 状态。
	defer r.setRunning(false)
	// 外层循环负责“启动探测”和“固定模式退出后重新探测”。
	for {
		// context 取消代表应用关闭，后台 runner 正常退出，不把关闭当作错误。
		if err := ctx.Err(); err != nil {
			return nil
		}
		// 每轮探测都先记录 starting，日志中能看到新一轮模式选择开始。
		r.recordState(RedisIngestSyncModeUnknown, RedisIngestSubStateStarting, "starting", "", "")
		// 启动优先尝试 Redis subscribe，只有订阅成功才进入最复杂的 subscribe 模式。
		sub, subErr := r.subscribeSource.Subscribe(ctx)
		if subErr == nil {
			// 订阅连上后先进入 backfill 阶段，补订阅建立前可能遗漏的队列数据。
			r.recordState(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeBackfill, "subscribe_connected", "", "")
			// subscribe 模式内部会处理断线、降级轮询和固定间隔重连。
			if err := r.runSubscribeMode(ctx, sub); err != nil && !errors.Is(err, context.Canceled) {
				// 非关停错误需要打 error 日志，便于定位 subscribe 模式为何退出。
				r.recordError("subscribe_stopped", err)
			}
			// subscribe 模式退出后重新回到启动探测，避免停在旧状态。
			continue
		}
		// 订阅失败不是最终失败，先记录降级原因，再尝试旧 Redis pull。
		r.recordWarning("subscribe_unavailable", subErr)
		// 启动探测第二步：Redis LPOP 成功就把长期模式固定为 redis_pull。
		redisCount, redisErr := r.serialPullAndWrite(ctx, RedisIngestSourceRedisPull, r.redisSource)
		if redisErr == nil {
			// 成功拉取说明远端恢复，清掉连续失败退避。
			r.failureBackoff.Reset()
			// 记录 redis_pull 长期模式，message_count 不进 status，避免状态字符串频繁变化。
			r.recordState(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullActive, pullStatus(redisCount), "", "")
			// redis_pull 模式内部负责 Redis 失败后的 HTTP 降级和 Redis 恢复。
			if err := r.runRedisPullMode(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 非关停错误需要记录，说明固定 redis_pull 模式异常退出。
				r.recordError("redis_pull_stopped", err)
			}
			// 模式退出后重新探测，允许后续升级到 subscribe。
			continue
		}
		if errors.Is(redisErr, errRedisIngestInboxWrite) {
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", redisErr) {
				return nil
			}
			continue
		}
		// Redis pull 失败继续降级到 HTTP，日志保留 Redis 失败原因。
		r.recordWarning("redis_pull_unavailable", redisErr)
		// 启动探测第三步：HTTP 成功就把长期模式固定为 http_pull。
		httpCount, httpErr := r.serialPullAndWrite(ctx, RedisIngestSourceHTTPPull, r.httpSource)
		if httpErr == nil {
			// HTTP 成功说明兜底链路可用，重置连续失败退避。
			r.failureBackoff.Reset()
			// 记录 http_pull 固定模式；该模式不再尝试 Redis/subscribe，符合启动选择规则。
			r.recordState(RedisIngestSyncModeHTTPPull, RedisIngestSubStateHTTPPullActive, pullStatus(httpCount), "", "")
			// http_pull 模式只按 HTTP 拉取，失败时用指数退避。
			if err := r.runHTTPPullMode(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 非关停错误需要记录，说明兜底模式自身异常退出。
				r.recordError("http_pull_stopped", err)
			}
			// http_pull 模式退出后重新探测，避免异常退出后永久停止。
			continue
		}
		// 三条远端入口都失败时合并错误，状态和日志能同时看到完整失败链。
		joined := errors.Join(subErr, redisErr, httpErr)
		// 连续启动失败使用指数退避，避免 CPA 故障时高频刷错误。
		delay := r.failureBackoff.NextDelay()
		// 记录最终启动失败，Status.LastError 展示合并后的失败原因。
		r.recordError("startup_failed", joined)
		// 下一次探测等待时间属于重复轮询细节，只放 debug，避免 error 日志成倍输出。
		logrus.WithError(joined).WithField("retry_after", delay.String()).Debug("redis ingest startup retry scheduled")
		// 等待可被 context 取消；取消时正常退出后台任务。
		if !r.sleep(ctx, delay) {
			return nil
		}
	}
}

func (r *RedisIngestRunner) runSubscribeMode(ctx context.Context, sub UsageSubscription) error {
	// current 始终指向当前有效订阅连接，重连后会替换为新连接。
	current := sub
	// subscribe 模式退出时关闭当前最新连接，避免重连后 defer 仍指向旧连接。
	defer func() { _ = current.Close() }()
	// 外层循环表示“每次订阅连接成功后的完整生命周期”。
	for {
		// 每次初次连接或重连成功后都做一次旧队列 backfill。
		if err := r.backfillAfterSubscribe(ctx); err != nil {
			_ = current.Close()
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
				return context.Canceled
			}
			return nil
		}
		// 内层循环专门接收当前订阅连接上的推送消息。
		for {
			// 进入接收态时记录状态；只有首次进入该子状态才会打 info。
			r.recordState(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeReceiving, "subscribing", "", "")
			// receiveSubscribeBatches 会阻塞等待首条消息，并按 1s 窗口批量写入。
			err := r.receiveSubscribeBatches(ctx, current)
			if err == nil {
				// nil 表示本轮批量已成功写入或窗口正常结束，继续等下一批订阅消息。
				continue
			}
			if errors.Is(err, context.Canceled) {
				// 应用关停时直接返回，避免进入降级轮询。
				return err
			}
			if errors.Is(err, errRedisIngestInboxWrite) {
				_ = current.Close()
				if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
					return context.Canceled
				}
				return nil
			}
			// 非取消错误代表订阅断开或写入失败，需要 error 日志。
			r.recordError("subscribe_disconnected", err)
			// 旧连接已不可用，先关闭再进入降级轮询，避免递归重连导致连接泄漏。
			_ = current.Close()
			// 降级轮询期间继续 Redis pull/HTTP pull，并按固定间隔 尝试恢复 subscribe。
			reconnected, err := r.runSubscribeDegradedPolling(ctx)
			if err != nil {
				// 降级轮询只有在 context 取消或不可恢复异常时返回错误。
				return err
			}
			// 重连成功后替换当前订阅连接。
			current = reconnected
			// 跳出内层接收循环，让外层先做 backfill，再回到订阅接收。
			break
		}
	}
}

func (r *RedisIngestRunner) backfillAfterSubscribe(ctx context.Context) error {
	// 订阅刚连接时进入 backfill 状态，补偿订阅建立前已在队列中的数据。
	r.recordState(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeBackfill, "subscribe_backfill", "", "")
	// backfill 优先复用旧 Redis queue 拉取，速度快且不经过 HTTP 管理接口。
	redisCount, redisBatches, err := r.drainBackfillSource(ctx, RedisIngestSourceRedisPull, r.redisSource)
	if err == nil {
		// Redis backfill 成功即清退避，后续失败重新从 1s 开始。
		r.failureBackoff.Reset()
		// 订阅成功后的补拉是关键一次性动作，info 只记录 drain 汇总，避免按批刷屏。
		logrus.WithFields(logrus.Fields{"message_count": redisCount, "batch_count": redisBatches}).Info("redis subscribe backfill used redis pull")
		return nil
	}
	if errors.Is(err, errRedisIngestInboxWrite) {
		return err
	}
	// Redis backfill 失败不影响已建立的 subscribe，需要继续尝试 HTTP backfill。
	r.recordWarning("subscribe_backfill_redis_failed", err)
	// Redis backfill 失败时用 HTTP queue 做兜底 backfill，同样 drain 到空批次。
	httpCount, httpBatches, err := r.drainBackfillSource(ctx, RedisIngestSourceHTTPPull, r.httpSource)
	if err == nil {
		// HTTP backfill 成功同样清退避，说明兜底链路健康。
		r.failureBackoff.Reset()
		// Redis backfill 失败后的 HTTP 补拉也是关键动作，info 只记录最终汇总。
		logrus.WithFields(logrus.Fields{"message_count": httpCount, "batch_count": httpBatches}).Info("redis subscribe backfill used http pull")
		return nil
	}
	if errors.Is(err, errRedisIngestInboxWrite) {
		return err
	}
	// 两种 backfill 都失败也不终止 subscribe，因为订阅连接已经能接收新消息。
	r.recordWarning("subscribe_backfill_failed", err)
	return nil
}

func (r *RedisIngestRunner) drainBackfillSource(ctx context.Context, sourceName string, source UsagePullSource) (int, int, error) {
	// backfill 必须 drain 到空批次，但每批立即落 inbox，避免远端已消费数据停留在内存。
	total := 0
	batches := 0
	for {
		count, err := r.serialPullAndWrite(ctx, sourceName, source)
		if err != nil {
			return total, batches, err
		}
		if count == 0 {
			return total, batches, nil
		}
		total += count
		batches++
	}
}

func (r *RedisIngestRunner) receiveSubscribeBatches(ctx context.Context, sub UsageSubscription) error {
	// 首条消息不设置 1s 超时，订阅模式应低 CPU 阻塞等待推送。
	first, err := sub.Receive(ctx)
	if err != nil {
		// 首条消息失败通常意味着订阅断开，交给上层进入降级轮询。
		return err
	}
	// 收到首条消息后立即建立批次，确保不会因为后续超时而丢消息。
	batch := []string{first}
	// 从首条消息开始最多等待 1s 聚合更多推送消息。
	windowCtx, cancel := context.WithTimeout(ctx, redisIngestSubscribeBatchWindow)
	// 无论是否满批，都释放 batch window 的 timer。
	defer cancel()
	// 批次未达到配置上限时继续尝试接收窗口内消息。
	for len(batch) < r.config.BatchSize {
		// 后续消息使用 1s window context，超时就把已有 batch 写入。
		message, err := sub.Receive(windowCtx)
		if err != nil {
			// 已有首条消息时，任何窗口结束/断开前都先把已有 batch 落库。
			if len(batch) > 0 {
				// 订阅消息仍按 raw usage JSON 原样写入 inbox，后续解码路径不变。
				inserted, writeErr := r.writer.Insert(ctx, RedisIngestSourceSubscribe, batch, timeutil.NormalizeStorageTime(r.now()))
				if writeErr != nil {
					// 写入失败说明本地 durable sink 不可用，不能继续消费更多远端消息。
					return fmt.Errorf("%w: %v", errRedisIngestInboxWrite, writeErr)
				}
				// 订阅循环收到和写入的数量只放 debug，不能在 info 刷屏。
				if logrus.IsLevelEnabled(logrus.DebugLevel) {
					logrus.WithFields(logrus.Fields{"message_count": len(batch), "inserted_count": inserted}).Debug("redis subscribe messages received")
				}
			}
			if errors.Is(err, context.DeadlineExceeded) {
				// 1s 聚合窗口正常结束，不视为订阅失败。
				return nil
			}
			// 非超时错误代表订阅连接问题，交给上层降级处理。
			return err
		}
		// 窗口内收到消息就追加到当前批次。
		batch = append(batch, message)
	}
	// 达到 batch size 后立即写入，不再等待 1s 窗口结束。
	inserted, err := r.writer.Insert(ctx, RedisIngestSourceSubscribe, batch, timeutil.NormalizeStorageTime(r.now()))
	if err != nil {
		// 写入失败说明本地 durable sink 不可用，不能继续消费更多远端消息。
		return fmt.Errorf("%w: %v", errRedisIngestInboxWrite, err)
	}
	// 订阅循环收到和写入的数量只放 debug，不能在 info 刷屏。
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithFields(logrus.Fields{"message_count": len(batch), "inserted_count": inserted}).Debug("redis subscribe messages received")
	}
	return nil
}

func (r *RedisIngestRunner) runSubscribeDegradedPolling(ctx context.Context) (UsageSubscription, error) {
	// 订阅断开后不是立即重连，而是 固定间隔后探测，避免断线时高频打 Redis。
	nextSubscribeRetryAt := r.now().Add(redisIngestRecoveryRetryInterval)
	// 降级轮询会持续运行，直到 subscribe 恢复或应用关闭。
	for {
		// 每轮先检查关停，避免关停时继续拉取远端数据。
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 标记 subscribe 模式下的降级轮询阶段，日志能区分“固定 redis_pull”和“订阅断线降级”。
		r.recordState(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeDegradedPolling, "subscribe_degraded_polling", "", "")
		// 到达固定间隔恢复点时尝试重新建立 subscribe。
		if !r.now().Before(nextSubscribeRetryAt) {
			// 只重连 subscribe，不在这里做 backfill；backfill 由 runSubscribeMode 外层统一执行。
			sub, err := r.subscribeSource.Subscribe(ctx)
			if err == nil {
				// 重连成功后回到 backfill 阶段，补断线期间可能遗漏的数据。
				r.recordState(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeBackfill, "subscribe_reconnected", "", "")
				// 恢复细节放 debug；子状态切换已经输出 info。
				logrus.Debug("redis ingest subscribe reconnected")
				return sub, nil
			}
			// 重连失败只记录 warning/error 日志，然后重新安排 固定间隔后再试。
			r.recordWarning("subscribe_reconnect_failed", err)
			// 使用 r.now 保持测试时钟一致。
			nextSubscribeRetryAt = r.now().Add(redisIngestRecoveryRetryInterval)
		}
		// 降级期间优先 Redis pull，尽量走原 Redis 队列路径。
		count, err := r.serialPullAndWrite(ctx, RedisIngestSourceRedisPull, r.redisSource)
		if err == nil {
			// Redis pull 成功表示降级拉取链路健康，清掉状态快照中的旧失败。
			r.recordAvailable(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeDegradedPolling, pullStatus(count), count)
			// Redis pull 成功表示降级拉取链路健康，清掉连续失败退避。
			r.failureBackoff.Reset()
			if count > 0 {
				// 有数据时立即下一轮，尽快把 backlog drain 到 inbox。
				continue
			}
			// 无数据时睡到 idle interval，但不能睡过下一次 subscribe 恢复探测点。
			if !r.sleepUntilRecovery(ctx, r.config.IdleInterval, nextSubscribeRetryAt) {
				return nil, context.Canceled
			}
			continue
		}
		if errors.Is(err, errRedisIngestInboxWrite) {
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
				return nil, context.Canceled
			}
			continue
		}
		// Redis pull 失败后保留错误，后面如果 HTTP 也失败要一起上报。
		redisErr := err
		// 记录 Redis 降级失败，日志中能看到正在从 Redis 继续降到 HTTP。
		r.recordWarning("subscribe_degraded_redis_failed", redisErr)
		// Redis 降级失败后尝试 HTTP pull，保证最终仍可从 CPA 管理接口取数。
		count, err = r.serialPullAndWrite(ctx, RedisIngestSourceHTTPPull, r.httpSource)
		if err == nil {
			// HTTP 成功表示兜底链路健康，清掉状态快照中的旧失败。
			r.recordAvailable(RedisIngestSyncModeSubscribe, RedisIngestSubStateSubscribeDegradedPolling, pullStatus(count), count)
			// HTTP 成功表示兜底链路健康，清掉失败退避。
			r.failureBackoff.Reset()
			if count > 0 {
				// 有数据时立即继续拉，避免 backlog 积压。
				continue
			}
			// 无数据时同样不能睡过 subscribe 恢复探测点。
			if !r.sleepUntilRecovery(ctx, r.config.IdleInterval, nextSubscribeRetryAt) {
				return nil, context.Canceled
			}
			continue
		}
		if errors.Is(err, errRedisIngestInboxWrite) {
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
				return nil, context.Canceled
			}
			continue
		}
		// HTTP 也失败时保留错误，与 Redis 错误合并后上报。
		httpErr := err
		// 连续失败使用指数退避，避免订阅断线且 CPA 故障时高频刷日志。
		delay := r.failureBackoff.NextDelay()
		// 同时记录 Redis 和 HTTP 两个失败原因，方便判断是局部还是整体故障。
		r.recordError("subscribe_degraded_failed", errors.Join(redisErr, httpErr))
		// 失败退避也不能超过下一次 subscribe 重连点，否则会破坏恢复间隔要求。
		if !r.sleepUntilRecovery(ctx, delay, nextSubscribeRetryAt) {
			return nil, context.Canceled
		}
	}
}

func (r *RedisIngestRunner) runRedisPullMode(ctx context.Context) error {
	// 固定 redis_pull 模式不会主动尝试 subscribe，只负责 Redis pull 与 HTTP 降级恢复。
	degraded := false
	// nextRedisRetryAt 只在 HTTP 降级阶段生效，用于 Redis 恢复探测。
	nextRedisRetryAt := time.Time{}
	for {
		// 每轮先响应应用关闭。
		if err := ctx.Err(); err != nil {
			return err
		}
		if degraded {
			// HTTP 降级阶段不递归调用其它模式，避免长时间故障/恢复后栈持续增长。
			r.recordState(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullDegradedHTTP, "redis_degraded_http", "", "")
			if !r.now().Before(nextRedisRetryAt) {
				// 到达固定间隔恢复点后尝试 Redis pull。
				count, err := r.serialPullAndWrite(ctx, RedisIngestSourceRedisPull, r.redisSource)
				if err == nil {
					// Redis 恢复成功后回到 active 阶段。
					r.failureBackoff.Reset()
					degraded = false
					r.recordState(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullActive, pullStatus(count), "", "")
					if count > 0 {
						continue
					}
					if !r.sleep(ctx, r.config.IdleInterval) {
						return context.Canceled
					}
					continue
				}
				// 恢复探测失败只记录 warning，HTTP 降级继续保持服务可用。
				if errors.Is(err, errRedisIngestInboxWrite) {
					if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
						return context.Canceled
					}
					continue
				}
				r.recordWarning("redis_recovery_failed", err)
				nextRedisRetryAt = r.now().Add(redisIngestRecoveryRetryInterval)
			}
			// 恢复点未到或恢复失败时，继续 HTTP pull 作为兜底。
			count, err := r.serialPullAndWrite(ctx, RedisIngestSourceHTTPPull, r.httpSource)
			if err == nil {
				// HTTP 成功后清掉状态快照中的旧失败。
				r.recordAvailable(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullDegradedHTTP, pullStatus(count), count)
				// HTTP 成功后清退避。
				r.failureBackoff.Reset()
				if count > 0 {
					continue
				}
				// 空批次按 idle interval 等待，但不能睡过下一次 Redis 恢复探测点。
				if !r.sleepUntilRecovery(ctx, r.config.IdleInterval, nextRedisRetryAt) {
					return context.Canceled
				}
				continue
			}
			// HTTP 降级路径也失败时进入退避。
			if errors.Is(err, errRedisIngestInboxWrite) {
				if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
					return context.Canceled
				}
				continue
			}
			delay := r.failureBackoff.NextDelay()
			r.recordError("redis_degraded_http_failed", err)
			// HTTP 失败退避不能睡过 Redis 恢复探测点。
			if !r.sleepUntilRecovery(ctx, delay, nextRedisRetryAt) {
				return context.Canceled
			}
			continue
		}

		// 标记 Redis pull 主动拉取阶段。
		r.recordState(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullActive, "redis_pull", "", "")
		// 固定 redis_pull 模式优先从 Redis 队列拉取。
		count, err := r.serialPullAndWrite(ctx, RedisIngestSourceRedisPull, r.redisSource)
		if err == nil {
			// Redis 成功表示固定 redis_pull 链路恢复，清掉状态快照中的旧失败。
			r.recordAvailable(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullActive, pullStatus(count), count)
			// Redis 成功后清掉失败退避。
			r.failureBackoff.Reset()
			if count > 0 {
				// 有数据时不 sleep，继续快速 drain。
				continue
			}
			// 空批次按 idle interval 等待，降低 CPU 和远端请求频率。
			if !r.sleep(ctx, r.config.IdleInterval) {
				return context.Canceled
			}
			continue
		}
		// Redis pull 失败后保存错误，如果 HTTP 也失败需要合并上报。
		if errors.Is(err, errRedisIngestInboxWrite) {
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
				return context.Canceled
			}
			continue
		}
		redisErr := err
		// 记录 Redis pull 失败，日志能看到准备降级到 HTTP。
		r.recordWarning("redis_pull_failed", redisErr)
		// Redis 失败时立即尝试 HTTP，不让 ingest 完全中断。
		count, err = r.serialPullAndWrite(ctx, RedisIngestSourceHTTPPull, r.httpSource)
		if err == nil {
			// HTTP 成功后清退避，并进入 redis_pull 的 HTTP 降级子状态。
			r.failureBackoff.Reset()
			degraded = true
			nextRedisRetryAt = r.now().Add(redisIngestRecoveryRetryInterval)
			// 记录降级结果，状态仍保持长期模式 redis_pull。
			r.recordState(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullDegradedHTTP, pullStatus(count), "", "")
			// HTTP 已经接管拉取，不能把 Redis 失败继续保留在状态快照中。
			r.recordAvailable(RedisIngestSyncModeRedisPull, RedisIngestSubStateRedisPullDegradedHTTP, pullStatus(count), count)
			if count > 0 {
				continue
			}
			if !r.sleepUntilRecovery(ctx, r.config.IdleInterval, nextRedisRetryAt) {
				return context.Canceled
			}
			continue
		}
		// HTTP 也失败时保留错误。
		if errors.Is(err, errRedisIngestInboxWrite) {
			if !r.pauseAfterInboxWriteFailure(ctx, "redis_inbox_write_failed", err) {
				return context.Canceled
			}
			continue
		}
		httpErr := err
		// 双失败时指数退避，避免持续故障高频请求。
		delay := r.failureBackoff.NextDelay()
		// 合并 Redis 与 HTTP 错误，状态快照和日志都能看到完整失败链。
		r.recordError("redis_pull_and_http_failed", errors.Join(redisErr, httpErr))
		// 失败等待可以被应用关闭打断。
		if !r.sleep(ctx, delay) {
			return context.Canceled
		}
	}
}

func (r *RedisIngestRunner) runHTTPPullMode(ctx context.Context) error {
	// 固定 http_pull 模式是启动时 Redis/subscribe 都不可用后的兜底模式。
	for {
		// 每轮先响应应用关闭。
		if err := ctx.Err(); err != nil {
			return err
		}
		// 标记 HTTP active 状态，日志中能看到当前长期模式固定为 HTTP。
		r.recordState(RedisIngestSyncModeHTTPPull, RedisIngestSubStateHTTPPullActive, "http_pull", "", "")
		// HTTP 模式只调用 HTTP source，不再尝试 Redis 或 subscribe。
		count, err := r.serialPullAndWrite(ctx, RedisIngestSourceHTTPPull, r.httpSource)
		if err == nil {
			// HTTP 成功后如果之前记录过失败，需要打一条恢复 info。
			r.recordRecovery(RedisIngestSyncModeHTTPPull, RedisIngestSubStateHTTPPullActive, "http_pull_recovered", count)
			// HTTP 成功后清掉连续失败退避。
			r.failureBackoff.Reset()
			if count > 0 {
				// 有数据时继续快速拉取，避免远端队列积压。
				continue
			}
			// 空批次时按 idle interval 休眠，避免高频空请求。
			if !r.sleep(ctx, r.config.IdleInterval) {
				return context.Canceled
			}
			continue
		}
		// HTTP 连续失败按指数退避增长到上限。
		delay := r.failureBackoff.NextDelay()
		// HTTP 是本模式唯一链路，每次失败都记录 error。
		r.recordError("http_pull_failed", err)
		// 退避等待可被应用关闭打断。
		if !r.sleep(ctx, delay) {
			return context.Canceled
		}
	}
}

func (r *RedisIngestRunner) serialPullAndWrite(ctx context.Context, sourceName string, source UsagePullSource) (int, error) {
	// 整个 pull/write 临界区串行执行，避免两个 goroutine 同时从远端消费同一队列。
	r.opMu.Lock()
	// pull/write 结束后释放串行锁。
	defer r.opMu.Unlock()
	// activeOps 表示正在执行一次真实拉取/写入，保留为内部诊断计数。
	r.setOperationRunning(true)
	// 无论成功失败都必须减少 activeOps。
	defer r.setOperationRunning(false)
	if source == nil {
		// source 缺失是配置/构造错误，直接返回给上层记录。
		return 0, fmt.Errorf("%s source is nil", sourceName)
	}
	// source 只负责拿 raw message，不负责 fallback、不负责落库。
	messages, err := source.Pull(ctx)
	if err != nil {
		// 拉取失败由调用方决定是否降级或退避。
		return 0, err
	}
	// 每次拉取方式和拉取数量属于可刷屏排查信息，只写 debug。
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithFields(logrus.Fields{"source": sourceName, "message_count": len(messages)}).Debug("redis ingest pulled usage messages")
	}
	if len(messages) == 0 {
		// 空批次不调用 writer，避免无意义 DB 事务。
		return 0, nil
	}
	// 所有来源的 raw usage message 都写入 redis_usage_inboxes，保持后续 decode/process 不变。
	inserted, err := r.writer.Insert(ctx, sourceName, messages, timeutil.NormalizeStorageTime(r.now()))
	if err != nil {
		// 落库失败必须返回给上层，不能吞掉导致消息丢失不可见。
		return 0, fmt.Errorf("%w: %v", errRedisIngestInboxWrite, err)
	}
	// 每次写入数量也属于可刷屏排查信息，只写 debug。
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithFields(logrus.Fields{"source": sourceName, "message_count": len(messages), "inserted_count": inserted}).Debug("redis ingest wrote usage messages")
	}
	// 返回实际插入行数，用于状态和日志。
	return inserted, nil
}

func (r *RedisIngestRunner) Status() Status {
	// 状态读取需要锁，避免与后台 runner 写状态发生数据竞争。
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{
		// Running 表示长生命周期 runner 是否启动。
		Running: r.running,
		// LastRunAt 取最近状态更新时间。
		LastRunAt: r.lastRunAt,
		// LastError 暴露最近一次最终失败。
		LastError: r.lastError,
		// LastWarning 暴露最近一次降级/可恢复失败。
		LastWarning: r.lastWarning,
		// LastStatus 兼容现有状态展示。
		LastStatus: r.lastStatus,
		// SyncRunning 表示远端 ingest 当前有可用链路；只有全部入口失败时才置 false。
		SyncRunning: r.running && r.lastError == "",
	}
}

func (r *RedisIngestRunner) validate() error {
	if r == nil {
		// nil runner 是调用方错误，不能继续执行。
		return fmt.Errorf("redis ingest runner is nil")
	}
	if r.subscribeSource == nil {
		// subscribe source 缺失会破坏启动优先级判断。
		return fmt.Errorf("redis ingest subscribe source is nil")
	}
	if r.redisSource == nil {
		// redis source 缺失会破坏 backfill 和 redis_pull 模式。
		return fmt.Errorf("redis ingest redis source is nil")
	}
	if r.httpSource == nil {
		// HTTP source 是最终兜底路径，不能缺失。
		return fmt.Errorf("redis ingest http source is nil")
	}
	if r.writer == nil {
		// writer 缺失会导致 raw message 无法进入 durable inbox。
		return fmt.Errorf("redis ingest writer is nil")
	}
	if r.config.IdleInterval <= 0 {
		// 非正 idle interval 会导致空队列时忙循环，强制退回 1s。
		r.config.IdleInterval = time.Second
	}
	if r.config.BatchSize <= 0 {
		// 非正 batch size 会破坏订阅批量循环，使用旧默认批量上限。
		r.config.BatchSize = 1000
	}
	if r.now == nil {
		// 测试可能覆盖 now；缺失时恢复生产默认值。
		r.now = time.Now
	}
	if r.sleep == nil {
		// 测试可能覆盖 sleep；缺失时恢复 context-aware sleep。
		r.sleep = sleepContext
	}
	if r.failureBackoff == nil {
		// backoff 缺失时恢复 1s 到 30s 的失败退避。
		r.failureBackoff = NewRedisIngestBackoff(time.Second, 30*time.Second)
	}
	return nil
}

func (r *RedisIngestRunner) setRunning(running bool) {
	// running 由 Run 生命周期设置，必须加锁保护。
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}

func (r *RedisIngestRunner) setOperationRunning(running bool) {
	// activeOps 可能被后台 runner 的不同拉取路径更新，必须加锁。
	r.mu.Lock()
	defer r.mu.Unlock()
	if running {
		// 开始一次 pull/write 操作时递增计数。
		r.activeOps++
		return
	}
	if r.activeOps > 0 {
		// 结束操作时递减；保护条件避免异常路径导致负数。
		r.activeOps--
	}
}

func (r *RedisIngestRunner) recordState(mode RedisIngestSyncMode, subState RedisIngestSubState, status string, lastError string, lastWarning string) {
	// 状态变更需要原子更新，避免 Status 读取到一半新一半旧的数据。
	r.mu.Lock()
	// 只有长期模式或子状态变化才属于 info 级别事件。
	modeChanged := r.mode != mode || r.subState != subState
	// 普通状态刷新不应清掉降级/失败原因，只有切到健康子状态才清理。
	nextError := r.lastError
	nextWarning := r.lastWarning
	if lastError != "" || lastWarning != "" {
		nextError = lastError
		nextWarning = lastWarning
	} else if modeChanged && redisIngestHealthyState(subState) {
		nextError = ""
		nextWarning = ""
	}
	// status/error/warning 的普通变化只用于 debug，不能刷 info。
	changed := modeChanged || r.lastStatus != status || r.lastError != nextError || r.lastWarning != nextWarning
	if !changed {
		r.mu.Unlock()
		return
	}
	// 记录长期模式。
	r.mode = mode
	// 记录长期模式内部阶段。
	r.subState = subState
	// 状态时间统一走 timeutil，保持项目时间处理一致。
	r.lastRunAt = timeutil.NormalizeStorageTime(r.now())
	// 更新对外状态字符串。
	r.lastStatus = status
	// 只有健康态或显式传入错误时才更新 error。
	r.lastError = nextError
	// 只有健康态或显式传入 warning 时才更新 warning。
	r.lastWarning = nextWarning
	r.mu.Unlock()
	fields := logrus.Fields{"mode": mode, "state": subState, "status": status}
	if modeChanged {
		// 只有模式选择、模式切换、关键子状态切换进入 info。
		logrus.WithFields(fields).Info("redis ingest state changed")
		return
	}
	if changed {
		// 每次拉取状态、空批次、计数类变化只放 debug。
		logrus.WithFields(fields).Debug("redis ingest state updated")
	}
}

func (r *RedisIngestRunner) pauseAfterInboxWriteFailure(ctx context.Context, status string, err error) bool {
	// 本地 inbox 写入失败时，暂停所有远端消费路径，避免继续消费后无法落库。
	delay := r.failureBackoff.NextDelay()
	r.recordError(status, err)
	logrus.WithError(err).WithField("retry_after", delay.String()).Debug("redis ingest inbox write retry scheduled")
	return r.sleep(ctx, delay)
}

func (r *RedisIngestRunner) recordAvailable(mode RedisIngestSyncMode, subState RedisIngestSubState, status string, count int) {
	// 任一拉取链路成功就代表远端 ingest 可用，状态快照不应继续保留旧失败。
	r.mu.Lock()
	hadError := r.lastError != ""
	hadFailure := hadError || r.lastWarning != ""
	if hadFailure {
		// 成功后清掉状态快照中的失败信息。
		r.lastRunAt = timeutil.NormalizeStorageTime(r.now())
		r.lastStatus = status
		r.lastError = ""
		r.lastWarning = ""
	}
	r.mu.Unlock()
	if hadError {
		// 只有从真正不可用恢复时记录 info；单次优先链路失败但 fallback 可用不刷 info。
		logrus.WithFields(logrus.Fields{"mode": mode, "state": subState, "status": status, "message_count": count}).Info("redis ingest recovered")
	}
}

func (r *RedisIngestRunner) recordRecovery(mode RedisIngestSyncMode, subState RedisIngestSubState, status string, count int) {
	// HTTP 固定模式恢复沿用可用链路记录逻辑。
	r.recordAvailable(mode, subState, status, count)
}

func (r *RedisIngestRunner) recordWarning(status string, err error) {
	// warning 状态代表可恢复失败或降级失败，状态快照需要记录。
	r.recordFailure(status, err, false)
}

func (r *RedisIngestRunner) recordError(status string, err error) {
	// error 状态代表当前路径最终失败，需要状态快照记录 LastError。
	r.recordFailure(status, err, true)
}

func (r *RedisIngestRunner) recordFailure(status string, err error, final bool) {
	if !shouldLogSyncError(err) {
		return
	}
	// warning/error 状态更新需要锁保护。
	r.mu.Lock()
	// 更新时间用于状态快照记录最近失败时间。
	r.lastRunAt = timeutil.NormalizeStorageTime(r.now())
	// status 标记具体失败阶段。
	r.lastStatus = status
	// 错误文本只保存字符串，不写 raw payload。
	errMessage := ""
	if err != nil {
		errMessage = err.Error()
	}
	if final {
		// 最终失败写 LastError，并清理 warning。
		r.lastError = errMessage
		r.lastWarning = ""
	} else {
		// 可恢复失败写 LastWarning，不覆盖 LastError。
		r.lastWarning = errMessage
	}
	r.mu.Unlock()
	entry := logrus.WithError(err).WithField("status", status)
	if final {
		// 当前路径最终失败使用 error；每次失败都记录，不能吞掉拉取故障。
		entry.Error("redis ingest failed")
		return
	}
	// 降级、恢复探测、backfill 等可恢复失败使用 warn；每次失败都记录。
	entry.Warn("redis ingest fallback failed")
}

func (r *RedisIngestRunner) sleepUntilRecovery(ctx context.Context, delay time.Duration, retryAt time.Time) bool {
	// 用 runner 时钟计算剩余时间，避免测试替换 now 后混用真实时钟。
	remaining := retryAt.Sub(r.now())
	if remaining > 0 && remaining < delay {
		// 如果下一次恢复探测更早，就截断当前 sleep。
		delay = remaining
	}
	// 实际等待仍交给可被 context 打断的 sleep 函数。
	return r.sleep(ctx, delay)
}

func redisIngestHealthyState(subState RedisIngestSubState) bool {
	switch subState {
	case RedisIngestSubStateSubscribeBackfill, RedisIngestSubStateSubscribeReceiving, RedisIngestSubStateRedisPullActive, RedisIngestSubStateHTTPPullActive:
		return true
	default:
		return false
	}
}

func pullStatus(count int) string {
	if count > 0 {
		// 非空批次记为 completed，兼容原有状态语义。
		return "completed"
	}
	// 空批次不是失败，单独标记 empty。
	return "empty"
}

func resolvePositiveDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		// 配置值有效时优先使用配置。
		return value
	}
	// 配置缺失或非法时使用安全默认值。
	return fallback
}
