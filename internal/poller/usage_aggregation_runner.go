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
)

// UsageAggregationKind 标识 runner 当前执行的两种公平调度 turn。
type UsageAggregationKind string

const (
	// UsageAggregationKindRollups 一次最多处理 Overview、Activity、Latency 各一页。
	UsageAggregationKindRollups UsageAggregationKind = "rollups"
	// UsageAggregationKindIdentity 沿用现有 Identity 分页和每行 cursor。
	UsageAggregationKindIdentity UsageAggregationKind = "identity"
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
	DebounceInterval time.Duration
	NowFunc          func() time.Time
}

// UsageAggregationRunner 在一个 goroutine 内公平调度共享 rollups 与既有 Identity。
type UsageAggregationRunner struct {
	db               *gorm.DB
	now              func() time.Time
	debounceInterval time.Duration

	// mu 只保护轻量内存目标和 Identity 分页状态，锁内不执行数据库操作。
	mu sync.Mutex
	// nextKind 固定在 rollups 与 Identity 之间轮转，防止任一 backlog 独占 writer。
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
	now := options.NowFunc
	if now == nil {
		now = time.Now
	}
	return &UsageAggregationRunner{
		db:                       db,
		now:                      now,
		debounceInterval:         debounceInterval,
		nextKind:                 UsageAggregationKindRollups,
		identityTargetGeneration: 1, // 启动时必须覆盖一次进程启动前已经存在的 identities。
		wake:                     make(chan struct{}, 1),
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
	// usage 提交既需要三个全局 rollup 追赶，也需要 Identity 覆盖一次最新事件。
	r.mu.Lock()
	if maxEventID > r.pendingTargetEventID {
		r.pendingTargetEventID = maxEventID
	}
	r.identityTargetGeneration++
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

	errorBackoffs := make(map[UsageAggregationKind]time.Duration, 2)
	var debounceTimer *time.Timer
	for {
		if ctx.Err() != nil {
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil
		}

		// 新目标即使遇到旧目标持续失败，也拥有独立 one-shot 窗口，避免健康 Rollup 一起冻结。
		r.mu.Lock()
		pendingRollupWindow := r.pendingTargetEventID > 0 && (!r.activeRound || r.pendingTargetEventID > r.activeTargetEventID)
		if pendingRollupWindow && debounceTimer == nil {
			debounceTimer = time.NewTimer(r.debounceInterval)
		}
		runnable := r.hasRunnableWorkLocked()
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
			result, err := r.runPreparedOnce(ctx)
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

		// 没有可执行 turn 时阻塞等待；这条路径不访问数据库。
		if debounceTimer == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-r.wake:
				continue
			}
		}
		select {
		case <-ctx.Done():
			debounceTimer.Stop()
			return nil
		case <-r.wake:
			// 新通知只更新 pending target；现有 timer 保持原到期时间。
			continue
		case <-debounceTimer.C:
			debounceTimer = nil
			r.freezePendingTarget()
		}
	}
}

// RunOnce 是同步测试/诊断入口：初始化或待冻结目标都立即处理，不真实等待 5 秒。
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
	return r.runPreparedOnce(ctx)
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startupTargetLoaded {
		return nil
	}
	r.startupTargetLoaded = true
	r.activeRound = true
	r.activeTargetEventID = targetEventID
	r.rollupsReachedTarget = targetEventID == 0
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

func (r *UsageAggregationRunner) hasRunnableWorkLocked() bool {
	return r.activeRound || r.identityWorkPendingLocked()
}

func (r *UsageAggregationRunner) identityWorkPendingLocked() bool {
	return r.identityAfterID != 0 || r.identityScanGeneration < r.identityTargetGeneration
}

func (r *UsageAggregationRunner) runPreparedOnce(ctx context.Context) (UsageAggregationRunResult, error) {
	r.mu.Lock()
	// 只有两类工作都存在时才遵循公平轮转；其中一类已经追平时直接执行另一类，避免空 turn 查询数据库。
	kind, runnable := r.nextRunnableKindLocked()
	targetEventID := r.activeTargetEventID
	activeRound := r.activeRound
	identityPending := r.identityWorkPendingLocked()
	identityAfterID := r.identityAfterID
	identityPassGeneration := r.identityScanGeneration
	if kind == UsageAggregationKindIdentity && identityPending && identityAfterID == 0 {
		identityPassGeneration = r.identityTargetGeneration
	}
	r.mu.Unlock()

	result := UsageAggregationRunResult{Kind: kind}
	if !runnable {
		return result, nil
	}
	writeDB := r.db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: ctx})
	// Identity 与 rollups 都会写唯一 writer，进入 turn 前先给 CPA inbox 让路。
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
			r.nextKind = UsageAggregationKindRollups
			r.mu.Unlock()
			return result, nil
		}
		batch, runErr := repository.AggregateUsageIdentityStatsBatch(transactionCtx, writeDB, now, identityAfterID)
		if runErr != nil {
			return result, runErr
		}
		r.mu.Lock()
		r.nextKind = UsageAggregationKindRollups
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

	default:
		return result, fmt.Errorf("unknown usage aggregation kind %q", kind)
	}
}

func (r *UsageAggregationRunner) nextRunnableKindLocked() (UsageAggregationKind, bool) {
	rollupsPending := r.activeRound
	identityPending := r.identityWorkPendingLocked()
	switch {
	case rollupsPending && identityPending:
		return r.nextKind, true
	case rollupsPending:
		return UsageAggregationKindRollups, true
	case identityPending:
		return UsageAggregationKindIdentity, true
	default:
		return r.nextKind, false
	}
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
	overviewHourly, overviewDaily, nextCursor := overview.BuildRows(events)
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
	if err := repository.ApplyUsageOverviewAggregationPage(ctx, writeDB, cursor, nextCursor, overviewHourly, overviewDaily, now); err != nil {
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
			hourly, daily, nextCursor := overview.BuildRows(events)
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
