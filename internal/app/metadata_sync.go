package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type MetadataSyncer interface {
	SyncMetadata(ctx context.Context) error
}

const metadataSyncRefreshDebounceDefault = 3 * time.Second

type MetadataSyncRunner struct {
	// syncer 是唯一真正执行 metadata 同步的服务入口。
	syncer MetadataSyncer
	// interval 是轮询模式下的固定同步间隔。
	interval time.Duration
	// refreshDebounce 是 refresh=true 后的最小合并等待窗口。
	refreshDebounce time.Duration
	// refreshRequests 用单元素缓冲合并密集 refresh 通知。
	refreshRequests chan struct{}
	// onStart 只给测试确认后台 runner 已启动，生产逻辑不依赖它。
	onStart func()
	// now 允许测试控制 debounce 时间判断，生产使用 time.Now。
	now func() time.Time

	// mu 保护 runner 状态和最近 refresh 时间。
	mu sync.Mutex
	// running 表示 metadata runner 当前是否处于 Run 生命周期内。
	running bool
	// notificationMode 表示 CPA 已支持通知，周期 tick 应该 no-op。
	notificationMode bool
	// lastRefreshRequestedAt 记录最后一条 refresh=true 的收到时间。
	lastRefreshRequestedAt time.Time
}

func NewMetadataSyncRunner(syncer MetadataSyncer, interval time.Duration) *MetadataSyncRunner {
	// 构造阶段只保存依赖，不主动访问 CPA 或数据库。
	return &MetadataSyncRunner{
		syncer:          syncer,
		interval:        interval,
		refreshDebounce: metadataSyncRefreshDebounceDefault,
		refreshRequests: make(chan struct{}, 1),
		now:             time.Now,
	}
}

// Run 启动独立 metadata 同步任务：默认轮询；收到 CPA 控制消息后进入通知模式，周期 tick 只保持空转。
func (r *MetadataSyncRunner) Run(ctx context.Context) error {
	// 启动前统一校验依赖和补齐测试可能清空的可选字段。
	if err := r.validate(); err != nil {
		return err
	}
	// 任务启动只打一条 info，后续周期 tick 不刷日志。
	logrus.Info("metadata sync task started")
	// running 状态用于诊断，不参与调度决策。
	r.setRunning(true)
	// 任何退出路径都必须清掉 running 状态。
	defer r.setRunning(false)
	// 测试 hook 在真正同步前触发，避免监听失败时 context 抢先取消导致测试偶发。
	if r.onStart != nil {
		r.onStart()
	}

	// 如果应用启动后立刻取消，就不再执行首次同步。
	if ctx.Err() != nil {
		return nil
	}
	// 无论后续是否进入通知模式，启动时先同步一次 metadata。
	r.runSync(ctx)

	// periodic 负责默认轮询模式的固定周期 tick。
	periodic := time.NewTimer(r.interval)
	// Run 退出时释放周期 timer。
	defer periodic.Stop()
	// debounce 只在收到 refresh=true 后创建。
	var debounce *time.Timer
	// debounceC 为 nil 时 select 不会监听 debounce 分支。
	var debounceC <-chan time.Time
	// Run 退出时释放可能仍在等待的 debounce timer。
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// 应用关闭是正常退出，不返回错误。
			return nil
		case <-periodic.C:
			// 周期 tick 到达后再检查一次 context，避免关停时多同步。
			if ctx.Err() != nil {
				return nil
			}
			// 通知模式下周期 tick 完全 no-op，避免继续轮询 CPA。
			if !r.isNotificationMode() {
				r.runSync(ctx)
			}
			// 无论是否 no-op，都继续维持 tick，方便降级回轮询后恢复节奏。
			periodic.Reset(r.interval)
		case <-r.refreshRequests:
			// 首个 refresh 请求创建 debounce timer。
			if debounce == nil {
				debounce = time.NewTimer(r.refreshDebounceDelay())
				debounceC = debounce.C
			} else {
				// 后续 refresh 请求重置 timer，实现最后一条消息后的 trailing debounce。
				resetTimer(debounce, r.refreshDebounceDelay())
			}
		case <-debounceC:
			// timer 触发时先清掉 channel 里被合并的 refresh 请求。
			r.drainRefreshRequests()
			// 如果 drain 期间又刷新了最后请求时间，就继续等剩余 debounce。
			if delay := r.refreshDebounceDelay(); delay > 0 {
				resetTimer(debounce, delay)
				continue
			}
			// refresh 只是无 payload 的失效通知；窗口到期后同步当前 metadata 即可。
			if ctx.Err() != nil {
				// 应用关停时不再发起 refresh 同步，避免关闭期多一次无效请求。
				return nil
			}
			// debounce 窗口稳定后只执行一次 SyncMetadata。
			r.runSync(ctx)
			// 同步完成后释放 debounce 状态，等待下一轮 refresh 创建新 timer。
			debounce = nil
			debounceC = nil
		}
	}
}

func (r *MetadataSyncRunner) MarkRefreshSupported() {
	// support_refresh=true 只切到通知模式，不立即同步 metadata。
	if r.setNotificationMode(true) {
		// CPA 明确支持 refresh 通知后，记录一次模式切换。
		logrus.WithField("source", "support_refresh").Info("metadata sync switched to notification mode")
	}
}

func (r *MetadataSyncRunner) RequestMetadataRefresh() {
	// nil runner 保护只为防御异常 wiring，正常路径不会触发。
	if r == nil {
		return
	}
	// refresh=true 同时证明通知可用，并刷新 trailing debounce 的起点。
	changed := r.recordRefreshRequest()
	if changed {
		// refresh=true 也能证明 CPA 通知可用，首次进入通知模式时记录。
		logrus.WithField("source", "refresh").Info("metadata sync switched to notification mode")
	}
	// channel 已满时说明已有 refresh 待处理，时间戳已更新即可。
	select {
	case r.refreshRequests <- struct{}{}:
	default:
	}
}

func (r *MetadataSyncRunner) MarkRefreshPollingRequired(reason string) {
	// usage ingest 降级或失败后必须回到轮询，等待下一条控制消息再进通知模式。
	if r.setNotificationMode(false) {
		// 回到轮询只是模式切换，真实错误由 usage ingest 自己记录。
		logrus.WithField("reason", reason).Info("metadata sync switched to polling mode")
	}
}

func (r *MetadataSyncRunner) validate() error {
	// nil runner 是调用方错误，不能继续启动。
	if r == nil {
		return fmt.Errorf("metadata sync runner is nil")
	}
	// syncer 缺失会导致 metadata 永远无法同步。
	if r.syncer == nil {
		return fmt.Errorf("metadata syncer is nil")
	}
	// 非正 interval 会导致轮询忙循环，必须拒绝。
	if r.interval <= 0 {
		return fmt.Errorf("metadata sync interval must be positive")
	}
	// 测试可能覆盖为非法值，运行前恢复默认 3s debounce。
	if r.refreshDebounce <= 0 {
		r.refreshDebounce = metadataSyncRefreshDebounceDefault
	}
	// 测试可能清空 channel，运行前恢复单缓冲合并队列。
	if r.refreshRequests == nil {
		r.refreshRequests = make(chan struct{}, 1)
	}
	// 测试可能清空时钟函数，运行前恢复生产时钟。
	if r.now == nil {
		r.now = time.Now
	}
	return nil
}

func (r *MetadataSyncRunner) runSync(ctx context.Context) {
	// 所有 metadata 同步都从这里串行调用 SyncMetadata。
	if err := r.syncer.SyncMetadata(ctx); err != nil {
		// 单次同步失败不退出 runner，下一次 tick 或 refresh 继续尝试。
		logrus.WithError(err).Error("metadata sync failed")
	}
}

func (r *MetadataSyncRunner) setRunning(running bool) {
	// running 状态可能被 Run goroutine 和测试读取，必须加锁。
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}

func (r *MetadataSyncRunner) setNotificationMode(enabled bool) bool {
	// nil runner 保护用于 observer 异常调用时不 panic。
	if r == nil {
		return false
	}
	// notificationMode 可能被 usage ingest 和 metadata runner 并发访问。
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := r.notificationMode != enabled
	r.notificationMode = enabled
	return changed
}

func (r *MetadataSyncRunner) recordRefreshRequest() bool {
	// refresh 请求需要在同一把锁内更新通知模式和最后请求时间。
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := !r.notificationMode
	r.notificationMode = true
	r.lastRefreshRequestedAt = r.currentTimeLocked()
	return changed
}

func (r *MetadataSyncRunner) isNotificationMode() bool {
	// 周期 tick 读取通知模式时需要加锁，避免和控制消息写入竞争。
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notificationMode
}

func (r *MetadataSyncRunner) drainRefreshRequests() {
	// drain 循环只清空已合并的信号，不改变最后 refresh 时间。
	for {
		select {
		case <-r.refreshRequests:
			// 已消费一条待处理信号，继续尝试清空缓冲。
		default:
			// channel 清空后返回，让 debounce 时间判断决定是否同步。
			return
		}
	}
}

func (r *MetadataSyncRunner) refreshDebounceDelay() time.Duration {
	// 读取最后 refresh 时间和 now 函数需要同一把锁保护。
	r.mu.Lock()
	defer r.mu.Unlock()
	// 没有 refresh 时间时使用完整 debounce，通常只发生在防御性路径。
	if r.lastRefreshRequestedAt.IsZero() {
		return r.refreshDebounce
	}
	// dueAt 表示最后一条 refresh 之后允许同步的最早时间。
	dueAt := r.lastRefreshRequestedAt.Add(r.refreshDebounce)
	// delay 是距离允许同步还需要等待的时间。
	delay := dueAt.Sub(r.currentTimeLocked())
	// 已超过 debounce 窗口时返回 0，让调用方立即同步。
	if delay < 0 {
		return 0
	}
	// 未到窗口时返回剩余等待时间。
	return delay
}

func (r *MetadataSyncRunner) currentTimeLocked() time.Time {
	// now 可被测试替换；调用方必须已经持有 r.mu。
	if r.now != nil {
		return r.now()
	}
	// 防御性兜底，正常 validate 后不会走到这里。
	return time.Now()
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	// timer 不接受负等待时间，负值统一按立即触发处理。
	if delay < 0 {
		delay = 0
	}
	// Reset 前必须 Stop 并清空可能已经触发的事件，避免 timer 旧事件串入新窗口。
	if !timer.Stop() {
		select {
		case <-timer.C:
			// 旧事件已被清空，可以安全 Reset。
		default:
			// 没有旧事件时直接 Reset。
		}
	}
	// 使用新的等待时间启动或延后 debounce timer。
	timer.Reset(delay)
}
