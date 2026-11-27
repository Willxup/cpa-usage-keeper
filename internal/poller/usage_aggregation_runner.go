package poller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// usageAggregationDefaultDebounceInterval 合并持续 CPA 提交，窗口内只提高目标而不重置计时。
	usageAggregationDefaultDebounceInterval = 5 * time.Second
	// usageAggregationInboxRetryInterval 给正在等待落库的 CPA 批次优先占用唯一 writer。
	usageAggregationInboxRetryInterval = 100 * time.Millisecond
	// usageAggregationErrorBackoffInitial 避免持续数据库错误形成 CPU 忙循环。
	usageAggregationErrorBackoffInitial = 100 * time.Millisecond
	// usageAggregationErrorBackoffMax 限制恢复等待，避免短暂错误让聚合长期停顿。
	usageAggregationErrorBackoffMax = 2 * time.Second
	// usageAggregationTransactionTimeout 给连接获取和单个短事务共同设置保守上限。
	usageAggregationTransactionTimeout = 30 * time.Second
	// usageAggregationEventPageSize 与 repository 的共享事件页硬上限保持一致。
	usageAggregationEventPageSize = 1000
	// usageAggregationQuotaHistoryActiveThrottle 对高频进行中限额周期做温和合并节流，避免每次事件都重扫长周期。
	usageAggregationQuotaHistoryActiveThrottle = 20 * time.Second
	// usageAggregationQuotaHistoryBatchSize 限制单 turn 重算周期数，避免阻塞其它聚合或 Pause 停稳。
	usageAggregationQuotaHistoryBatchSize = repository.DefaultQuotaHistoryRecomputeBatchSize
)

// UsageAggregationKind 标识 runner 当前执行的公平调度 turn。
type UsageAggregationKind string

const (
	// UsageAggregationKindRollups 一次最多处理 Overview、Activity、Latency 各一页。
	UsageAggregationKindRollups UsageAggregationKind = "rollups"
	// UsageAggregationKindIdentity 沿用现有 Identity 分页和每行 cursor。
	UsageAggregationKindIdentity UsageAggregationKind = "identity"
	// UsageAggregationKindQuotaHistory 低优先级推进限额历史标脏游标与有界周期持久化重算。
	UsageAggregationKindQuotaHistory UsageAggregationKind = "quota_history"
)

// UsageAggregationRunResult 描述一次 turn 是否写入数据或为 CPA inbox 让路。
type UsageAggregationRunResult struct {
	Kind UsageAggregationKind
	// Processed 表示本 turn 至少提交了一页 rollup，或 Identity 实际处理了行。
	Processed bool
	// DeferredForInbox 表示 writer 前发现 CPA 批次等待落库，本 turn 主动停止。
	DeferredForInbox bool
}

// UsageAggregationRunnerOptions 只开放测试需要控制的时钟和时间窗口，不增加用户配置项。
type UsageAggregationRunnerOptions struct {
	DebounceInterval           time.Duration
	QuotaHistoryActiveThrottle time.Duration
	NowFunc                    func() time.Time
}

// UsageAggregationRunner 在一个 goroutine 内公平调度共享 rollups、Identity 与限额历史持久化。
type UsageAggregationRunner struct {
	db                         *gorm.DB
	now                        func() time.Time
	debounceInterval           time.Duration
	quotaHistoryActiveThrottle time.Duration
	// workGate 保护普通 turn 的完整 Reader 预读、计算、事务提交和内存状态更新。
	workGate aggregationWorkGate

	// mu 只保护轻量内存目标和 Identity/限额历史分页状态，锁内不执行数据库操作。
	mu sync.Mutex
	// nextKind 固定在 rollups、Identity、quota_history 之间轮转，防止任一 backlog 独占 writer。
	nextKind UsageAggregationKind
	// startupTargetLoaded 保证完全静默时启动只查询一次 MAX(id)。
	startupTargetLoaded bool
	// activeRound 表示当前已有冻结目标；后续窗口到期时可以把目标提高到更新的已提交事件。
	activeRound bool
	// activeTargetEventID 是当前窗口内所有共享和 fallback 页面共同追赶的固定上界。
	activeTargetEventID int64
	// rollupsReachedTarget 避免已追平后为了判断完成再次查询数据库。
	rollupsReachedTarget bool
	// pendingTargetEventID 保存尚未冻结窗口内收到的最大已提交事件 ID。
	pendingTargetEventID int64

	// identityAfterID 保留现有一轮 Identity ID 分页的内存 cursor。
	identityAfterID int64
	// identityTargetGeneration 表示 usage/metadata 通知要求覆盖的最新扫描代际。
	identityTargetGeneration uint64
	// identityScanGeneration 是当前分页扫描从头开始时捕获的通知代际。
	identityScanGeneration uint64

	// quotaHistoryActive 表示库中已存在限额周期，无周期的实例跳过事件标脏扫描以保持零额外开销。
	quotaHistoryActive bool
	// quotaHistoryPending 标记存在待扫描事件、dirty 周期或 90 天热表范围内的待回填周期。
	quotaHistoryPending bool
	// quotaHistoryTargetGeneration 记录任意事件提交或观测变更通知推进的最新代际，防止并发通知被旧 turn 覆盖。
	quotaHistoryTargetGeneration uint64
	// quotaHistoryObservationGeneration 记录观测/周期结构变更通知代际，防止旧 turn 冷却状态覆盖新观测立即重算。
	quotaHistoryObservationGeneration uint64
	// quotaHistoryRetryAfter 记录全部剩余周期均处于节流或等待偏快事件时的最早可重算时刻。
	quotaHistoryRetryAfter time.Time
	// quotaHistoryLastComputedAt 记录进行中周期最近一次完成重算的时刻，供解除偏快等待时恢复常规节流。
	quotaHistoryLastComputedAt map[int64]time.Time
	// quotaHistoryActiveReleaseAt 记录进行中周期温和节流的下次可重算时刻。
	quotaHistoryActiveReleaseAt map[int64]time.Time
	// quotaHistoryFutureReleaseAt 记录存在偏快事件（timestamp >= now）周期等待时钟追上的时刻，避免阻塞后续周期。
	quotaHistoryFutureReleaseAt map[int64]time.Time

	// wake 只表达状态已经变化，容量 1 会自然合并连续通知。
	wake chan struct{}
}

// NewUsageAggregationRunner 创建生产默认 5 秒窗口的单 writer runner。
func NewUsageAggregationRunner(db *gorm.DB) *UsageAggregationRunner {
	return NewUsageAggregationRunnerWithOptions(db, UsageAggregationRunnerOptions{})
}

// NewUsageAggregationRunnerWithOptions 创建可控制时钟和 debounce 的内部测试 runner。
func NewUsageAggregationRunnerWithOptions(db *gorm.DB, options UsageAggregationRunnerOptions) *UsageAggregationRunner {
	debounceInterval := options.DebounceInterval
	if debounceInterval <= 0 {
		debounceInterval = usageAggregationDefaultDebounceInterval
	}
	quotaHistoryThrottle := options.QuotaHistoryActiveThrottle
	if quotaHistoryThrottle <= 0 {
		quotaHistoryThrottle = usageAggregationQuotaHistoryActiveThrottle
	}
	now := options.NowFunc
	if now == nil {
		now = time.Now
	}
	return &UsageAggregationRunner{
		db:                         db,
		now:                        now,
		debounceInterval:           debounceInterval,
		quotaHistoryActiveThrottle: quotaHistoryThrottle,
		nextKind:                   UsageAggregationKindRollups,
		identityTargetGeneration:    1, // 启动时必须覆盖一次进程启动前已经存在的 identities。
		quotaHistoryLastComputedAt:  make(map[int64]time.Time),
		quotaHistoryActiveReleaseAt: make(map[int64]time.Time),
		quotaHistoryFutureReleaseAt: make(map[int64]time.Time),
		wake:                        make(chan struct{}, 1),
	}
}

// NotifyUsageEventsCommitted 在事务成功后只提高内存目标并非阻塞唤醒 runner。
func (r *UsageAggregationRunner) NotifyUsageEventsCommitted(events []entities.UsageEvent) {
	if r == nil {
		return
	}
	maxEventID := int64(0)
	for _, event := range events {
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
	}
	if maxEventID <= 0 {
		return
	}
	// usage 提交既需要三个全局 rollup 追赶，也需要 Identity 与限额历史覆盖最新事件。
	r.mu.Lock()
	if maxEventID > r.pendingTargetEventID {
		r.pendingTargetEventID = maxEventID
	}
	r.identityTargetGeneration++
	if r.quotaHistoryActive {
		r.quotaHistoryPending = true
		r.quotaHistoryTargetGeneration++
		// 新事件到达时立即解除全局等待，让游标和无关账号立刻推进；仍在冷却的单周期由按周期字典独立跳过。
		r.quotaHistoryRetryAfter = time.Time{}
	}
	r.mu.Unlock()
	r.signalWake()
}

// NotifyUsageIdentitiesChanged 只提高 Identity 代际，不创建或重置 rollups timer。
func (r *UsageAggregationRunner) NotifyUsageIdentitiesChanged() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.identityTargetGeneration++
	r.mu.Unlock()
	r.signalWake()
}

// NotifyQuotaHistoryChanged 在配额观测写入或周期删除提交后非阻塞唤醒限额历史持久化汇总。
func (r *UsageAggregationRunner) NotifyQuotaHistoryChanged() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.quotaHistoryActive = true
	r.quotaHistoryPending = true
	r.quotaHistoryTargetGeneration++
	r.quotaHistoryObservationGeneration++
	// 观测变更（如新降档、边界升级、删周期）应立即解除节流等待与周期冷却记录。
	r.quotaHistoryRetryAfter = time.Time{}
	clear(r.quotaHistoryLastComputedAt)
	clear(r.quotaHistoryActiveReleaseAt)
	clear(r.quotaHistoryFutureReleaseAt)
	r.mu.Unlock()
	r.signalWake()
}

// Run 启动时立即追平一次，之后只在真实通知、timer 或内部重试时工作。
func (r *UsageAggregationRunner) Run(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("usage aggregation runner is nil")
	}
	if r.db == nil {
		return fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 启动 MAX(id) 失败时使用同一有界退避重试；成功后永不在静默状态重复查询。
	startupBackoff := usageAggregationErrorBackoffInitial
	for {
		if err := r.ensureStartupTarget(ctx); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				return nil
			}
			logrus.WithError(err).WithField("aggregation_kind", UsageAggregationKindRollups).Error("usage aggregation batch failed")
			if !r.waitForRetry(ctx, startupBackoff) {
				return nil
			}
			startupBackoff = min(startupBackoff*2, usageAggregationErrorBackoffMax)
			continue
		}
		break
	}

	errorBackoffs := make(map[UsageAggregationKind]time.Duration, 3)
	var debounceTimer *time.Timer
	for {
		if ctx.Err() != nil {
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil
		}

		// 新目标即使遇到旧目标持续失败，也拥有独立 one-shot 窗口，避免健康 Rollup 一起冻结。
		now := r.now()
		r.mu.Lock()
		pendingRollupWindow := r.pendingTargetEventID > 0 && (!r.activeRound || r.pendingTargetEventID > r.activeTargetEventID)
		if pendingRollupWindow && debounceTimer == nil {
			debounceTimer = time.NewTimer(r.debounceInterval)
		}
		runnable := r.hasRunnableWorkLocked(now)
		quotaThrottleWait := time.Duration(0)
		if !runnable && r.quotaHistoryPending && !r.quotaHistoryRetryAfter.IsZero() && now.Before(r.quotaHistoryRetryAfter) {
			quotaThrottleWait = r.quotaHistoryRetryAfter.Sub(now)
		}
		r.mu.Unlock()

		// Identity 处理期间 timer 也可能到期，每个短 turn 之间优先冻结当前窗口。
		if debounceTimer != nil {
			select {
			case <-debounceTimer.C:
				debounceTimer = nil
				r.freezePendingTarget()
				continue
			default:
			}
		}

		if runnable {
			result, err := r.runPreparedOnce(ctx, false)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
					return nil
				}
				logrus.WithError(err).WithField("aggregation_kind", result.Kind).Error("usage aggregation batch failed")
				r.advanceAfterFailure(result.Kind)
				backoff := errorBackoffs[result.Kind]
				if backoff == 0 {
					backoff = usageAggregationErrorBackoffInitial
				}
				if !r.waitForRetry(ctx, backoff) {
					return nil
				}
				errorBackoffs[result.Kind] = min(backoff*2, usageAggregationErrorBackoffMax)
				continue
			}
			delete(errorBackoffs, result.Kind)
			if result.DeferredForInbox {
				if !r.waitForRetry(ctx, usageAggregationInboxRetryInterval) {
					return nil
				}
			}
			continue
		}

		var throttleTimer *time.Timer
		var throttleC <-chan time.Time
		if quotaThrottleWait > 0 {
			throttleTimer = time.NewTimer(quotaThrottleWait)
			throttleC = throttleTimer.C
		}

		// 没有可执行 turn 时阻塞等待；这条路径不访问数据库。
		if debounceTimer == nil {
			select {
			case <-ctx.Done():
				if throttleTimer != nil {
					throttleTimer.Stop()
				}
				return nil
			case <-r.wake:
				if throttleTimer != nil {
					throttleTimer.Stop()
				}
				continue
			case <-throttleC:
				r.mu.Lock()
				r.quotaHistoryRetryAfter = time.Time{}
				r.mu.Unlock()
				continue
			}
		}
		select {
		case <-ctx.Done():
			debounceTimer.Stop()
			if throttleTimer != nil {
				throttleTimer.Stop()
			}
			return nil
		case <-r.wake:
			if throttleTimer != nil {
				throttleTimer.Stop()
			}
			// 新通知只更新 pending target；现有 timer 保持原到期时间。
			continue
		case <-throttleC:
			r.mu.Lock()
			r.quotaHistoryRetryAfter = time.Time{}
			r.mu.Unlock()
		case <-debounceTimer.C:
			if throttleTimer != nil {
				throttleTimer.Stop()
			}
			debounceTimer = nil
			r.freezePendingTarget()
		}
	}
}

// RunOnce 是同步测试/诊断入口：初始化或待冻结目标都立即处理，不真实等待 5 秒或进行中周期节流。
func (r *UsageAggregationRunner) RunOnce(ctx context.Context) (UsageAggregationRunResult, error) {
	if r == nil {
		return UsageAggregationRunResult{}, fmt.Errorf("usage aggregation runner is nil")
	}
	if r.db == nil {
		return UsageAggregationRunResult{}, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.ensureStartupTarget(ctx); err != nil {
		return UsageAggregationRunResult{Kind: UsageAggregationKindRollups}, err
	}
	// 直接调用不启动 timer；它把已经收到的最大 ID 作为当前固定目标。
	r.freezePendingTarget()
	return r.runPreparedOnce(ctx, true)
}

func (r *UsageAggregationRunner) ensureStartupTarget(ctx context.Context) error {
	r.mu.Lock()
	loaded := r.startupTargetLoaded
	r.mu.Unlock()
	if loaded {
		return nil
	}
	targetEventID, err := repository.LoadUsageAggregationTargetEventID(ctx, r.db)
	if err != nil {
		return err
	}
	hasQuotaCycles, err := repository.HasAnyQuotaCycles(ctx, r.db)
	if err != nil {
		return err
	}
	quotaHistoryPending := false
	if hasQuotaCycles {
		quotaHistoryPending, err = repository.HasPendingQuotaHistoryWork(ctx, r.db, r.now())
		if err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startupTargetLoaded {
		return nil
	}
	r.startupTargetLoaded = true
	r.activeRound = true
	r.activeTargetEventID = targetEventID
	r.rollupsReachedTarget = targetEventID == 0
	if hasQuotaCycles {
		r.quotaHistoryActive = true
		if quotaHistoryPending || r.pendingTargetEventID > targetEventID {
			r.quotaHistoryPending = true
			r.quotaHistoryTargetGeneration++
		}
	}
	// 查询期间到达且 ID 更大的通知留给下一窗口；已被 MAX 覆盖的通知无需再跑一轮。
	if r.pendingTargetEventID <= targetEventID {
		r.pendingTargetEventID = 0
	}
	return nil
}

func (r *UsageAggregationRunner) freezePendingTarget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingTargetEventID <= 0 {
		return
	}
	// 重复的旧通知不应降低或重启当前目标。
	if r.activeRound && r.pendingTargetEventID <= r.activeTargetEventID {
		r.pendingTargetEventID = 0
		return
	}
	// 新窗口到期后直接提高当前目标；落后的类型保留旧水位，健康类型可继续追新数据。
	r.activeRound = true
	r.activeTargetEventID = r.pendingTargetEventID
	r.pendingTargetEventID = 0
	r.rollupsReachedTarget = false
}

func (r *UsageAggregationRunner) hasRunnableWorkLocked(now time.Time) bool {
	return r.activeRound || r.identityWorkPendingLocked() || r.quotaHistoryRunnableLocked(now, false)
}

func (r *UsageAggregationRunner) quotaHistoryRunnableLocked(now time.Time, ignoreActiveThrottle bool) bool {
	if !r.quotaHistoryPending {
		return false
	}
	if ignoreActiveThrottle || r.quotaHistoryRetryAfter.IsZero() {
		return true
	}
	return !now.Before(r.quotaHistoryRetryAfter)
}

func (r *UsageAggregationRunner) identityWorkPendingLocked() bool {
	return r.identityAfterID != 0 || r.identityScanGeneration < r.identityTargetGeneration
}

func (r *UsageAggregationRunner) runPreparedOnce(ctx context.Context, ignoreActiveThrottle bool) (UsageAggregationRunResult, error) {
	leave, err := r.workGate.enter(ctx)
	if err != nil {
		return UsageAggregationRunResult{}, err
	}
	defer leave()
	r.mu.Lock()
	// 只有在多类工作都存在时才遵循公平轮转；其它类已追平时直接执行待处理类，避免空 turn 查询数据库。
	kind, runnable := r.nextRunnableKindLocked(ignoreActiveThrottle)
	targetEventID := r.activeTargetEventID
	activeRound := r.activeRound
	identityPending := r.identityWorkPendingLocked()
	identityAfterID := r.identityAfterID
	identityPassGeneration := r.identityScanGeneration
	if kind == UsageAggregationKindIdentity && identityPending && identityAfterID == 0 {
		identityPassGeneration = r.identityTargetGeneration
	}
	quotaHistoryTargetGen := r.quotaHistoryTargetGeneration
	quotaHistoryObservationGen := r.quotaHistoryObservationGeneration
	r.mu.Unlock()

	result := UsageAggregationRunResult{Kind: kind}
	if !runnable {
		return result, nil
	}
	writeDB := r.db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: ctx})
	// Identity、rollups 与限额历史都会写唯一 writer，进入 turn 前先给 CPA inbox 让路。
	hasInbox, err := repository.HasProcessableRedisUsageInbox(ctx, writeDB)
	if err != nil {
		return result, err
	}
	if hasInbox {
		result.DeferredForInbox = true
		return result, nil
	}

	now := r.now()
	transactionCtx, cancelTransaction := boundedUsageAggregationContext(ctx, usageAggregationTransactionTimeout)
	defer cancelTransaction()

	switch kind {
	case UsageAggregationKindRollups:
		processed, deferred, reachedTarget, runErr := r.runRollupsTurn(transactionCtx, writeDB, activeRound, targetEventID, now)
		result.Processed = processed
		result.DeferredForInbox = deferred
		if runErr != nil {
			return result, runErr
		}
		r.mu.Lock()
		r.nextKind = UsageAggregationKindIdentity
		if activeRound && r.activeRound && r.activeTargetEventID == targetEventID && reachedTarget {
			r.rollupsReachedTarget = true
		}
		r.completeRollupRoundIfReadyLocked()
		r.mu.Unlock()
		return result, nil

	case UsageAggregationKindIdentity:
		if !identityPending {
			r.mu.Lock()
			r.nextKind = UsageAggregationKindQuotaHistory
			r.mu.Unlock()
			return result, nil
		}
		batch, runErr := repository.AggregateUsageIdentityStatsBatch(transactionCtx, writeDB, now, identityAfterID)
		if runErr != nil {
			return result, runErr
		}
		r.mu.Lock()
		r.nextKind = UsageAggregationKindQuotaHistory
		if batch.ReachedEnd {
			r.identityAfterID = 0
			r.identityScanGeneration = identityPassGeneration
		} else {
			r.identityAfterID = batch.LastIdentityID
			r.identityScanGeneration = identityPassGeneration
		}
		// 尾页期间出现新通知时，pending=true 要求下一次从头重扫；未到尾页也属于仍有有界工作。
		identityStillPending := r.identityWorkPendingLocked()
		r.mu.Unlock()
		result.Processed = batch.ProcessedIdentities > 0 || !batch.ReachedEnd || identityStillPending
		return result, nil

	case UsageAggregationKindQuotaHistory:
		processed, deferred, stillPending, retryAfter, runErr := r.runQuotaHistoryTurn(transactionCtx, writeDB, now, ignoreActiveThrottle, quotaHistoryObservationGen)
		result.Processed = processed
		result.DeferredForInbox = deferred
		if runErr != nil {
			return result, runErr
		}
		r.mu.Lock()
		r.nextKind = UsageAggregationKindRollups
		targetAdvanced := r.quotaHistoryTargetGeneration != quotaHistoryTargetGen
		observationAdvanced := r.quotaHistoryObservationGeneration != quotaHistoryObservationGen
		if !stillPending {
			// 仅在代际未前进时清除待处理标记，避免覆盖执行期间并发到达的事件或观测通知。
			if !targetAdvanced {
				r.quotaHistoryPending = false
			}
			r.quotaHistoryRetryAfter = time.Time{}
		} else {
			r.quotaHistoryPending = true
			// 若执行期间并发到达新事件或观测变更，不得写入旧节流等待时刻，让下一 turn 立即检查。
			if !retryAfter.IsZero() && !targetAdvanced && !observationAdvanced {
				r.quotaHistoryRetryAfter = retryAfter
			} else {
				r.quotaHistoryRetryAfter = time.Time{}
			}
		}
		r.mu.Unlock()
		return result, nil

	default:
		return result, fmt.Errorf("unknown usage aggregation kind %q", kind)
	}
}

func (r *UsageAggregationRunner) nextRunnableKindLocked(ignoreActiveThrottle bool) (UsageAggregationKind, bool) {
	quotaRunnable := r.quotaHistoryPending
	if quotaRunnable && !ignoreActiveThrottle && !r.quotaHistoryRetryAfter.IsZero() && r.now().Before(r.quotaHistoryRetryAfter) {
		quotaRunnable = false
	}
	pendingByKind := map[UsageAggregationKind]bool{
		UsageAggregationKindRollups:      r.activeRound,
		UsageAggregationKindIdentity:     r.identityWorkPendingLocked(),
		UsageAggregationKindQuotaHistory: quotaRunnable,
	}
	order := []UsageAggregationKind{
		UsageAggregationKindRollups,
		UsageAggregationKindIdentity,
		UsageAggregationKindQuotaHistory,
	}
	startIdx := 0
	for i, kind := range order {
		if kind == r.nextKind {
			startIdx = i
			break
		}
	}
	for offset := 0; offset < len(order); offset++ {
		candidate := order[(startIdx+offset)%len(order)]
		if pendingByKind[candidate] {
			return candidate, true
		}
	}
	return r.nextKind, false
}

func (r *UsageAggregationRunner) runQuotaHistoryTurn(ctx context.Context, writeDB *gorm.DB, _ time.Time, ignoreActiveThrottle bool, observationGen uint64) (processed bool, deferred bool, stillPending bool, retryAfter time.Time, err error) {
	// 1. 先按 usage_events.id 推进标脏游标一页；Reader 预读后、申请唯一 Writer 前先为 CPA inbox 让路。
	scanRes, err := repository.AdvanceQuotaHistoryDirtyCursorWithDefer(ctx, r.db, r.now(), usageAggregationEventPageSize, func(checkCtx context.Context) (bool, error) {
		return r.deferForInbox(checkCtx, writeDB)
	})
	if err != nil {
		return false, false, true, time.Time{}, err
	}
	if scanRes.DeferredForInbox {
		return false, true, true, time.Time{}, nil
	}
	if scanRes.ScannedEvents > 0 {
		processed = true
	}
	if len(scanRes.DirtyCycleIDs) > 0 {
		r.mu.Lock()
		for _, cycleID := range scanRes.DirtyCycleIDs {
			if _, waitingFuture := r.quotaHistoryFutureReleaseAt[cycleID]; waitingFuture {
				delete(r.quotaHistoryFutureReleaseAt, cycleID)
				if lastAt, ok := r.quotaHistoryLastComputedAt[cycleID]; ok && r.quotaHistoryActiveThrottle > 0 {
					r.quotaHistoryActiveReleaseAt[cycleID] = lastAt.Add(r.quotaHistoryActiveThrottle)
				}
			}
		}
		r.mu.Unlock()
	}

	// 2. 游标推进完成后重新采样结算时刻，确保刚被游标扫过的正常同钟请求落入 [start, now)。
	recomputeNow := r.now()

	// 3. 写周期统计前先检查 CPA inbox 是否等待落库。
	if deferred, err = r.deferForInbox(ctx, writeDB); err != nil || deferred {
		return processed, deferred, true, time.Time{}, err
	}

	// 4. 按周期跳过仍在等待偏快事件时刻或处于进行中温和节流窗口的周期，不阻塞后续待算周期。
	var throttledIDs []int64
	throttledSet := make(map[int64]struct{})
	var earliestWake time.Time
	noteWake := func(candidate time.Time) {
		if candidate.IsZero() {
			return
		}
		if earliestWake.IsZero() || candidate.Before(earliestWake) {
			earliestWake = candidate
		}
	}
	r.mu.Lock()
	for cycleID, releaseAt := range r.quotaHistoryFutureReleaseAt {
		if recomputeNow.Before(releaseAt) {
			throttledSet[cycleID] = struct{}{}
			throttledIDs = append(throttledIDs, cycleID)
			noteWake(releaseAt)
		} else {
			delete(r.quotaHistoryFutureReleaseAt, cycleID)
		}
	}
	if !ignoreActiveThrottle && r.quotaHistoryActiveThrottle > 0 {
		for cycleID, releaseAt := range r.quotaHistoryActiveReleaseAt {
			if recomputeNow.Before(releaseAt) {
				if _, exists := throttledSet[cycleID]; !exists {
					throttledSet[cycleID] = struct{}{}
					throttledIDs = append(throttledIDs, cycleID)
				}
				noteWake(releaseAt)
			} else {
				delete(r.quotaHistoryActiveReleaseAt, cycleID)
			}
		}
	}
	r.mu.Unlock()

	cycles, hasMoreCycles, err := repository.SelectQuotaCyclesForUsageSync(ctx, r.db, recomputeNow, usageAggregationQuotaHistoryBatchSize, throttledIDs)
	if err != nil {
		return processed, false, true, time.Time{}, err
	}

	hadUnappliedCycle := false
	hasFuturePending := false
	for _, cycle := range cycles {
		if deferred, err = r.deferForInbox(ctx, writeDB); err != nil || deferred {
			return processed, deferred, true, time.Time{}, err
		}
		computed, computeErr := repository.ComputeQuotaCyclePersistedUsage(ctx, r.db, cycle, recomputeNow, 0)
		if computeErr != nil {
			return processed, false, true, time.Time{}, computeErr
		}
		// Reader 整周期扫描期间可能新到达 CPA inbox 批次，申请唯一 Writer 回写前再次让路。
		if deferred, err = r.deferForInbox(ctx, writeDB); err != nil || deferred {
			return processed, deferred, true, time.Time{}, err
		}
		applied, applyErr := repository.ApplyComputedQuotaCycleUsage(ctx, r.db, computed, recomputeNow, false)
		if applyErr != nil {
			return processed, false, true, time.Time{}, applyErr
		}
		if applied {
			processed = true
			if !computed.NextFutureEventAt.IsZero() {
				hasFuturePending = true
				futureWake := computed.NextFutureEventAt.Add(time.Millisecond)
				noteWake(futureWake)
				r.mu.Lock()
				if r.quotaHistoryObservationGeneration == observationGen {
					if computed.Active {
						r.quotaHistoryLastComputedAt[cycle.ID] = recomputeNow
					}
					r.quotaHistoryFutureReleaseAt[cycle.ID] = futureWake
				}
				r.mu.Unlock()
			} else if computed.Active && r.quotaHistoryActiveThrottle > 0 {
				r.mu.Lock()
				// 若本 turn 执行期间已有新观测通知到达，不写回旧冷却时间，避免挡住新观测立即重算。
				if r.quotaHistoryObservationGeneration == observationGen {
					r.quotaHistoryLastComputedAt[cycle.ID] = recomputeNow
					r.quotaHistoryActiveReleaseAt[cycle.ID] = recomputeNow.Add(r.quotaHistoryActiveThrottle)
				}
				r.mu.Unlock()
			}
		} else {
			// 并发 boundary_version 变更或周期删改导致本次未落库，保持待处理以供下一 turn 重算。
			hadUnappliedCycle = true
		}
	}

	if scanRes.HasMore || hasMoreCycles || hadUnappliedCycle {
		return processed, false, true, time.Time{}, nil
	}
	if hasFuturePending {
		return processed, false, true, earliestWake, nil
	}
	// 若本轮跳过了节流或等待偏快事件的周期，检查它们是否仍为 dirty，避免静默丢失更新。
	if len(throttledIDs) > 0 {
		pending, checkErr := repository.HasPendingQuotaHistoryWork(ctx, r.db, recomputeNow)
		if checkErr != nil {
			return processed, false, true, time.Time{}, checkErr
		}
		if pending {
			return processed, false, true, earliestWake, nil
		}
	}
	return processed, false, false, time.Time{}, nil
}

func (r *UsageAggregationRunner) runRollupsTurn(ctx context.Context, writeDB *gorm.DB, activeRound bool, targetEventID int64, now time.Time) (bool, bool, bool, error) {
	if !activeRound {
		return false, false, true, nil
	}
	snapshot, err := repository.LoadUsageAggregationCheckpointSnapshot(ctx, r.db)
	if err != nil {
		return false, false, false, err
	}
	if snapshot.OverviewCursor >= targetEventID && snapshot.ActivityCursor >= targetEventID && snapshot.LatencyCursor >= targetEventID {
		return false, false, true, nil
	}
	if snapshot.Equal() {
		return r.runSharedRollupsPage(ctx, writeDB, snapshot.OverviewCursor, targetEventID, now)
	}
	return r.runFallbackRollupsPages(ctx, writeDB, snapshot, targetEventID, now)
}

func (r *UsageAggregationRunner) runSharedRollupsPage(ctx context.Context, writeDB *gorm.DB, cursor, targetEventID int64, now time.Time) (bool, bool, bool, error) {
	events, err := repository.LoadUsageAggregationEventPage(ctx, r.db, cursor, targetEventID, usageAggregationEventPageSize)
	if err != nil {
		return false, false, false, err
	}
	if len(events) == 0 {
		return false, false, false, fmt.Errorf("shared usage aggregation page is empty before target %d from cursor %d", targetEventID, cursor)
	}

	// 三类纯计算复用同一份只读事件；各自错误只跳过自己，成功结果仍可用独立事务提交。
	// 三类聚合共享同一实际事件页终点；Overview 费用缺失不能把其它独立水位误推到 0。
	nextCursor := events[len(events)-1].ID
	overviewHourly, overviewDaily, _, overviewErr := overview.BuildRows(events)
	activityRows, activityErr := activity.BuildRows(events, now)
	latencyRows, latencyErr := latency.BuildRows(events, now)

	processed := false
	pageErrors := make([]error, 0, 3)
	if deferred, err := r.deferForInbox(ctx, writeDB); err != nil || deferred {
		if err != nil {
			pageErrors = append(pageErrors, err)
		}
		return processed, deferred, false, errors.Join(pageErrors...)
	}
	if overviewErr != nil {
		pageErrors = append(pageErrors, overviewErr)
	} else if err := repository.ApplyUsageOverviewAggregationPage(ctx, writeDB, cursor, nextCursor, overviewHourly, overviewDaily, now); err != nil {
		pageErrors = append(pageErrors, err)
	} else {
		processed = true
	}
	if deferred, err := r.deferForInbox(ctx, writeDB); err != nil || deferred {
		if err != nil {
			pageErrors = append(pageErrors, err)
		}
		return processed, deferred, false, errors.Join(pageErrors...)
	}
	if activityErr != nil {
		pageErrors = append(pageErrors, activityErr)
	} else if err := repository.ApplyUsageActivityAggregationPage(ctx, writeDB, cursor, nextCursor, activityRows, now); err != nil {
		pageErrors = append(pageErrors, err)
	} else {
		processed = true
	}
	if deferred, err := r.deferForInbox(ctx, writeDB); err != nil || deferred {
		if err != nil {
			pageErrors = append(pageErrors, err)
		}
		return processed, deferred, false, errors.Join(pageErrors...)
	}
	if latencyErr != nil {
		pageErrors = append(pageErrors, latencyErr)
	} else if err := repository.ApplyUsageLatencyAggregationPage(ctx, writeDB, cursor, nextCursor, latencyRows, now); err != nil {
		pageErrors = append(pageErrors, err)
	} else {
		processed = true
	}
	if err := errors.Join(pageErrors...); err != nil {
		return processed, false, false, err
	}
	return processed, false, nextCursor >= targetEventID, nil
}

func (r *UsageAggregationRunner) runFallbackRollupsPages(ctx context.Context, writeDB *gorm.DB, snapshot repository.UsageAggregationCheckpointSnapshot, targetEventID int64, now time.Time) (bool, bool, bool, error) {
	processed := false
	pageErrors := make([]error, 0, 3)
	if snapshot.OverviewCursor < targetEventID {
		events, err := repository.LoadUsageAggregationEventPage(ctx, r.db, snapshot.OverviewCursor, targetEventID, usageAggregationEventPageSize)
		if err != nil {
			pageErrors = append(pageErrors, err)
		} else if len(events) == 0 {
			pageErrors = append(pageErrors, fmt.Errorf("overview fallback page is empty before target %d", targetEventID))
		} else {
			hourly, daily, nextCursor, buildErr := overview.BuildRows(events)
			if buildErr != nil {
				pageErrors = append(pageErrors, buildErr)
			} else {
				if deferred, deferErr := r.deferForInbox(ctx, writeDB); deferErr != nil || deferred {
					if deferErr != nil {
						pageErrors = append(pageErrors, deferErr)
					}
					return processed, deferred, false, errors.Join(pageErrors...)
				}
				if applyErr := repository.ApplyUsageOverviewAggregationPage(ctx, writeDB, snapshot.OverviewCursor, nextCursor, hourly, daily, now); applyErr != nil {
					pageErrors = append(pageErrors, applyErr)
				} else {
					snapshot.OverviewCursor = nextCursor
					processed = true
				}
			}
		}
	}
	if snapshot.ActivityCursor < targetEventID {
		events, err := repository.LoadUsageAggregationEventPage(ctx, r.db, snapshot.ActivityCursor, targetEventID, usageAggregationEventPageSize)
		if err != nil {
			pageErrors = append(pageErrors, err)
		} else if len(events) == 0 {
			pageErrors = append(pageErrors, fmt.Errorf("activity fallback page is empty before target %d", targetEventID))
		} else {
			rows, buildErr := activity.BuildRows(events, now)
			if buildErr != nil {
				pageErrors = append(pageErrors, buildErr)
			} else {
				nextCursor := events[len(events)-1].ID
				if deferred, deferErr := r.deferForInbox(ctx, writeDB); deferErr != nil || deferred {
					if deferErr != nil {
						pageErrors = append(pageErrors, deferErr)
					}
					return processed, deferred, false, errors.Join(pageErrors...)
				}
				if applyErr := repository.ApplyUsageActivityAggregationPage(ctx, writeDB, snapshot.ActivityCursor, nextCursor, rows, now); applyErr != nil {
					pageErrors = append(pageErrors, applyErr)
				} else {
					snapshot.ActivityCursor = nextCursor
					processed = true
				}
			}
		}
	}
	if snapshot.LatencyCursor < targetEventID {
		events, err := repository.LoadUsageAggregationEventPage(ctx, r.db, snapshot.LatencyCursor, targetEventID, usageAggregationEventPageSize)
		if err != nil {
			pageErrors = append(pageErrors, err)
		} else if len(events) == 0 {
			pageErrors = append(pageErrors, fmt.Errorf("latency fallback page is empty before target %d", targetEventID))
		} else {
			rows, buildErr := latency.BuildRows(events, now)
			if buildErr != nil {
				pageErrors = append(pageErrors, buildErr)
			} else {
				nextCursor := events[len(events)-1].ID
				if deferred, deferErr := r.deferForInbox(ctx, writeDB); deferErr != nil || deferred {
					if deferErr != nil {
						pageErrors = append(pageErrors, deferErr)
					}
					return processed, deferred, false, errors.Join(pageErrors...)
				}
				if applyErr := repository.ApplyUsageLatencyAggregationPage(ctx, writeDB, snapshot.LatencyCursor, nextCursor, rows, now); applyErr != nil {
					pageErrors = append(pageErrors, applyErr)
				} else {
					snapshot.LatencyCursor = nextCursor
					processed = true
				}
			}
		}
	}
	reached := snapshot.OverviewCursor >= targetEventID && snapshot.ActivityCursor >= targetEventID && snapshot.LatencyCursor >= targetEventID
	return processed, false, reached, errors.Join(pageErrors...)
}

func (r *UsageAggregationRunner) deferForInbox(ctx context.Context, writeDB *gorm.DB) (bool, error) {
	hasInbox, err := repository.HasProcessableRedisUsageInbox(ctx, writeDB)
	if err != nil {
		return false, err
	}
	return hasInbox, nil
}

func (r *UsageAggregationRunner) completeRollupRoundIfReadyLocked() {
	// Rollup 轮次只由三个全局水位结束；Identity 保留自己的代际和分页状态，不能冻结下一批 usage 目标。
	if !r.activeRound || !r.rollupsReachedTarget {
		return
	}
	completedTarget := r.activeTargetEventID
	r.activeRound = false
	r.activeTargetEventID = 0
	r.rollupsReachedTarget = false
	// 重复或延迟到达的旧通知不应制造一个没有新数据的 5 秒窗口。
	if r.pendingTargetEventID <= completedTarget {
		r.pendingTargetEventID = 0
	}
}

func (r *UsageAggregationRunner) advanceAfterFailure(kind UsageAggregationKind) {
	if kind == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch kind {
	case UsageAggregationKindRollups:
		r.nextKind = UsageAggregationKindIdentity
	case UsageAggregationKindIdentity:
		r.nextKind = UsageAggregationKindQuotaHistory
	case UsageAggregationKindQuotaHistory:
		r.nextKind = UsageAggregationKindRollups
	}
}

func (r *UsageAggregationRunner) signalWake() {
	if r == nil || r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *UsageAggregationRunner) waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-r.wake:
		return true
	case <-timer.C:
		return true
	}
}

// boundedUsageAggregationContext 忽略 parent cancel，同时保留更早 deadline 并添加最大上限。
func boundedUsageAggregationContext(parent context.Context, maxDuration time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	base := context.WithoutCancel(parent)
	deadline := time.Now().Add(maxDuration)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	return context.WithDeadline(base, deadline)
}
