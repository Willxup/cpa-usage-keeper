package poller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// usageAggregationInboxRetryInterval 控制 backlog 仍存在时再次检查的最短间隔。
	usageAggregationInboxRetryInterval = 100 * time.Millisecond
	// usageAggregationHeaderRetryInterval 控制 quota appender 拒绝后再次投递的有界间隔。
	usageAggregationHeaderRetryInterval = 100 * time.Millisecond
	// usageAggregationErrorBackoffInitial 避免持续数据库错误形成 CPU 忙循环。
	usageAggregationErrorBackoffInitial = 100 * time.Millisecond
	// usageAggregationErrorBackoffMax 限制恢复等待，避免新数据长期得不到重试。
	usageAggregationErrorBackoffMax = 2 * time.Second
	// usageAggregationTransactionTimeout 给连接获取和一个短事务共同设置保守上限。
	usageAggregationTransactionTimeout = 30 * time.Second
	// usageAggregationHeaderCursorTimeout 限制事务提交后的普通 header gate 查询等待。
	usageAggregationHeaderCursorTimeout = time.Second
	// usageAggregationShutdownFlushTimeout 限制关闭时 best-effort header cursor 查询等待。
	usageAggregationShutdownFlushTimeout = time.Second
	// usageAggregationPendingHeaderLimit 限制内存中不同 auth_index 的等待快照数量。
	usageAggregationPendingHeaderLimit = 1000
)

// UsageAggregationKind 标识单 writer runner 当前执行的独立聚合事务。
type UsageAggregationKind string

const (
	// UsageAggregationKindOverview 表示既有 hourly/daily 与 Overview checkpoint 事务。
	UsageAggregationKindOverview UsageAggregationKind = "overview"
	// UsageAggregationKindActivity 表示新 Activity rows 与独立 checkpoint 事务。
	UsageAggregationKindActivity UsageAggregationKind = "activity"
	// UsageAggregationKindIdentity 表示一页既有 usage identity 聚合事务。
	UsageAggregationKindIdentity UsageAggregationKind = "identity"
)

// UsageAggregationRunResult 描述一次调度是否让路或真正处理了数据。
type UsageAggregationRunResult struct {
	// Kind 是本次选择的独立聚合类型；inbox 让路时为空。
	Kind UsageAggregationKind
	// Processed 表示本次事务处理了数据，或确认仍有同类有界工作必须继续调度。
	Processed bool
	// DeferredForInbox 表示发现前台 inbox backlog 后没有启动聚合事务。
	DeferredForInbox bool
	// HeaderRetryPending 表示 ready snapshot 被 appender 拒绝后仍保留在内存。
	HeaderRetryPending bool
}

// pendingUsageHeaderSnapshot 把 quota snapshot 与所需 Overview event cursor 绑定。
type pendingUsageHeaderSnapshot struct {
	// snapshot 是按 auth_index 合并后等待投递的最新值。
	snapshot quota.UsageHeaderSnapshot
	// requiredOverviewEventID 是该 snapshot 所属提交批次的最大 event ID。
	requiredOverviewEventID int64
}

// UsageAggregationRunner 串行公平调度 Overview、Activity 和 Identity 三类短事务。
type UsageAggregationRunner struct {
	// db 保持项目现有单连接 SQLite writer。
	db *gorm.DB
	// headerAppender 接收已经越过 Overview checkpoint gate 的 quota snapshots。
	headerAppender quota.UsageHeaderSnapshotAppender
	// now 为每个短事务提供同一次固定时间。
	now func() time.Time

	// mu 保护 notifier 与 runner goroutine 共享的轻量内存状态。
	mu sync.Mutex
	// nextKind 记录下一次应该获得 writer 的聚合类型。
	nextKind UsageAggregationKind
	// identityAfterID 只在一轮 identity ID 扫描期间保存在内存。
	identityAfterID int64
	// identityTargetGeneration 记录 usage/metadata 通知要求完成的最新 Identity 扫描代际。
	identityTargetGeneration uint64
	// identityScanGeneration 记录当前分页扫描开始时已经观察到的通知代际。
	identityScanGeneration uint64
	// pendingHeaders 按 auth_index 只保留等待 gate 的最新 snapshot。
	pendingHeaders map[string]pendingUsageHeaderSnapshot
	// wake 合并连续通知，调用前台永远不等待后台消费。
	wake chan struct{}
}

// NewUsageAggregationRunner 创建从 Overview 开始公平轮转的单 writer runner。
func NewUsageAggregationRunner(db *gorm.DB, headerAppender quota.UsageHeaderSnapshotAppender) *UsageAggregationRunner {
	// 构造时只初始化内存状态，不启动额外 SQLite writer goroutine。
	return &UsageAggregationRunner{
		// 复用 App 已打开的单连接数据库。
		db: db,
		// 保留现有 quota service 的非阻塞 appender 接口。
		headerAppender: headerAppender,
		// 生产默认使用系统时钟，repository 内部会再统一存储时区。
		now: time.Now,
		// 第一个短事务固定选择 Overview，优先满足旧表与 header gate。
		nextKind: UsageAggregationKindOverview,
		// 空 map 用于接收 usage 提交后的 header snapshots。
		pendingHeaders: make(map[string]pendingUsageHeaderSnapshot),
		// 容量 1 只表达“有工作”，不按通知次数累计任务。
		wake: make(chan struct{}, 1),
	}
}

// NotifyUsageEventsCommitted 在 usage 事务提交后只更新内存目标并发送非阻塞 wake。
func (r *UsageAggregationRunner) NotifyUsageEventsCommitted(events []entities.UsageEvent, snapshots []quota.UsageHeaderSnapshot) {
	// nil runner 不应由生产构造，但保持 notifier 调用可安全忽略。
	if r == nil {
		return
	}
	// 本批最大 event ID 是所有 snapshot 共用的 Overview checkpoint gate。
	maxEventID := int64(0)
	// 只遍历已提交事件，不读取数据库或等待聚合。
	for _, event := range events {
		// 自增 ID 更大时推进本批 gate 目标。
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
	}
	// 没有已提交 ID 时不能建立可靠 gate，只发送 wake 供已有 checkpoint catch-up。
	if maxEventID > 0 {
		// droppedHeaders 记录容量已满时主动舍弃的新 auth_index 数量。
		droppedHeaders := 0
		// 加锁只覆盖内存 map 合并，不执行 SQLite 或 quota 工作。
		r.mu.Lock()
		// 每批已提交 usage 都要求 Identity 在当前分页轮结束后覆盖一次最新代际。
		r.identityTargetGeneration++
		// 同批 snapshots 按 auth_index 分别合并。
		for _, snapshot := range snapshots {
			// auth_index 去除首尾空白后作为稳定合并键。
			authIndex := strings.TrimSpace(snapshot.AuthIndex)
			// 空 auth_index 无法关联 quota identity，直接跳过。
			if authIndex == "" {
				continue
			}
			// 读取同一 auth_index 当前等待值以比较新旧观测时间。
			existing, exists := r.pendingHeaders[authIndex]
			// 更旧 snapshot 不覆盖已经等待的更新值。
			if exists && snapshot.ObservedAt.Before(existing.snapshot.ObservedAt) {
				continue
			}
			// 容量已满时仍允许更新现有 auth_index，但拒绝继续增长新的 map key。
			if !exists && len(r.pendingHeaders) >= usageAggregationPendingHeaderLimit {
				droppedHeaders++
				continue
			}
			// clone HTTP headers，避免调用方复用 map 后改变内存快照。
			snapshot.Headers = snapshot.Headers.Clone()
			// 规范化 snapshot 自身的 auth_index，确保 appender 收到稳定键。
			snapshot.AuthIndex = authIndex
			// 新值同时记录所属提交批次的 Overview gate。
			r.pendingHeaders[authIndex] = pendingUsageHeaderSnapshot{snapshot: snapshot, requiredOverviewEventID: maxEventID}
		}
		// map 更新完成立即释放锁。
		r.mu.Unlock()
		// 容量保护只影响 header quota 新鲜度，不能阻塞或回滚已经提交的 usage events。
		if droppedHeaders > 0 {
			logrus.WithField("dropped_snapshot_count", droppedHeaders).Warn("usage aggregation pending header capacity reached")
		}
	}
	// capacity=1 channel 合并连续通知，default 分支保证前台不阻塞。
	r.signalWake()
}

// NotifyUsageIdentitiesChanged 在 metadata 提交后只唤醒已有 runner，不同步执行历史补算。
func (r *UsageAggregationRunner) NotifyUsageIdentitiesChanged() {
	// nil runner 允许可选 notifier 调用安全返回。
	if r == nil {
		return
	}
	// metadata 新建或恢复 identity 时推进目标代际，确保当前分页轮结束后从 ID 头部重扫。
	r.mu.Lock()
	// 代际只表达至少需要一次新扫描，不保存任何数据库状态。
	r.identityTargetGeneration++
	// 更新完成立即释放锁，notifier 不执行 SQLite 工作。
	r.mu.Unlock()
	// capacity=1 wake 会与 usage 提交通知自然合并。
	r.signalWake()
}

// Run 在一个 goroutine 内持续执行短事务，并在空闲时等待合并 wake。
func (r *UsageAggregationRunner) Run(ctx context.Context) error {
	// nil runner 无法进入后台生命周期。
	if r == nil {
		return fmt.Errorf("usage aggregation runner is nil")
	}
	// nil 数据库必须在启动时明确失败，不能静默空转。
	if r.db == nil {
		return fmt.Errorf("database is nil")
	}
	// nil context 统一降级为 Background，保持 Runner 接口可安全调用。
	if ctx == nil {
		ctx = context.Background()
	}
	// 退出路径最后尝试一次 ready snapshot 投递；内部会忽略 cancel 但保留有界 deadline。
	defer r.flushReadyHeaderSnapshotsOnShutdown(ctx)
	// startup wake 补偿进程启动前或上次退出前尚未追平的 checkpoints。
	r.signalWake()
	// errorBackoffs 按 kind 独立保存持续失败退避，互不覆盖或重置。
	errorBackoffs := make(map[UsageAggregationKind]time.Duration, 3)

	// 外层循环只在 startup、通知或内部重试需要工作时进入调度。
	for {
		// 空闲时不轮询数据库，等待 context 关闭或容量 1 wake。
		select {
		case <-ctx.Done():
			// context 关闭时当前没有事务，直接正常退出。
			return nil
		case <-r.wake:
			// 收到 wake 后开始一轮公平调度。
		}

		// 三类连续空 batch 才能证明当前一整轮没有工作。
		emptyKinds := 0
		// 内层循环每次仍只执行一个 RunOnce 短事务。
		for emptyKinds < 3 {
			// 每个新事务开始前先响应已经发生的 shutdown。
			if ctx.Err() != nil {
				return nil
			}
			// RunOnce 自己会在取 writer 前重新检查 processable inbox。
			result, err := r.RunOnce(ctx)
			// 某一类事务失败时记录错误并保留对应数据库 checkpoint。
			if err != nil {
				// 只有错误链真正来自 parent cancel 时才按正常 shutdown 静默退出。
				if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
					return nil
				}
				logrus.WithError(err).WithField("aggregation_kind", result.Kind).Error("usage aggregation batch failed")
				// 当前 kind 首次失败时从初始退避开始。
				errorBackoff := errorBackoffs[result.Kind]
				// map 尚无记录时填入初始间隔。
				if errorBackoff == 0 {
					errorBackoff = usageAggregationErrorBackoffInitial
				}
				// 后台循环把下一个事务配额交给其它类型，避免永久错误阻塞全部聚合。
				r.advanceAfterBackgroundFailure(result.Kind)
				// 错误后等待有界 backoff；shutdown 会立即中断等待。
				if !r.waitForRetry(ctx, errorBackoff) {
					return nil
				}
				// 当前 kind 下一次错误等待翻倍，但不超过固定上限。
				errorBackoffs[result.Kind] = min(errorBackoff*2, usageAggregationErrorBackoffMax)
				// 错误不能算作空 batch，继续让其它 kind 获得事务配额。
				emptyKinds = 0
				continue
			}
			// 当前 kind 成功后只清除自己的退避状态，不影响其它失败类型。
			delete(errorBackoffs, result.Kind)
			// inbox backlog 时本次没有启动聚合事务。
			if result.DeferredForInbox {
				// 短暂等待 process runner 提交；其 notifier wake 也可提前结束等待。
				if !r.waitForRetry(ctx, usageAggregationInboxRetryInterval) {
					return nil
				}
				// backlog 可能仍存在，继续从事务前检查重新判断。
				emptyKinds = 0
				continue
			}
			// Overview 没有 event 但 header 仍被拒绝时，按有界间隔继续当前 wake。
			if result.HeaderRetryPending && !result.Processed {
				// 新 wake 或 shutdown 都可以提前结束等待，禁止形成 quota 重试忙循环。
				if !r.waitForRetry(ctx, usageAggregationHeaderRetryInterval) {
					return nil
				}
				// header 仍属于未完成后台工作，不能累计为空 batch。
				emptyKinds = 0
				continue
			}
			// 处理到数据时立即继续下一 kind，但 RunOnce 会再次检查 inbox。
			if result.Processed {
				emptyKinds = 0
				continue
			}
			// 成功空 batch 表示当前 kind 暂时已经追平。
			emptyKinds++
		}
	}
}

// signalWake 合并重复通知并保证前台调用永不等待后台消费。
func (r *UsageAggregationRunner) signalWake() {
	// nil wake 只可能来自非标准零值 runner，防御性跳过发送。
	if r == nil || r.wake == nil {
		return
	}
	// channel 有空位时写入一个工作信号。
	select {
	case r.wake <- struct{}{}:
		// 成功写入后由后台循环消费。
	default:
		// 已有未消费信号时直接合并本次通知。
	}
}

// waitForRetry 等待退避、下一次 wake 或 shutdown，并返回是否应继续调度。
func (r *UsageAggregationRunner) waitForRetry(ctx context.Context, delay time.Duration) bool {
	// timer 只覆盖当前一次有界等待。
	timer := time.NewTimer(delay)
	// 提前退出时停止 timer，避免资源滞留到自然触发。
	defer timer.Stop()
	// 任一信号都结束当前等待。
	select {
	case <-ctx.Done():
		// shutdown 优先结束后台循环。
		return false
	case <-r.wake:
		// 新提交数据或 metadata 变化触发立即重试。
		return true
	case <-timer.C:
		// 没有通知时按有界间隔自行重试。
		return true
	}
}

// RunOnce 最多执行一个短写事务，并在事务前让路给 processable inbox。
func (r *UsageAggregationRunner) RunOnce(ctx context.Context) (UsageAggregationRunResult, error) {
	// result 默认表示没有选择事务也没有处理数据。
	result := UsageAggregationRunResult{}
	// nil runner 无法调度任何聚合。
	if r == nil {
		return result, fmt.Errorf("usage aggregation runner is nil")
	}
	// nil 数据库不能执行 inbox 检查或聚合事务。
	if r.db == nil {
		return result, fmt.Errorf("database is nil")
	}
	// nil context 统一降级为 Background，避免 GORM WithContext panic。
	if ctx == nil {
		ctx = context.Background()
	}
	// 本轮的 inbox 门禁、聚合事务和提交后 cursor 都共同控制 writer 进度，不能被页面 reader 饱和反向阻塞。
	// Write scope 只绑定当前 RunOnce；其它 Overview、Activity、Analysis 等纯查询仍由 dbresolver 自动分流。
	// clause 后重新建 session，保留 writer ConnPool，同时阻止门禁查询的 Model/Where 泄漏到后续聚合。
	writeDB := r.db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: ctx})
	// 每个写事务之前都在 writer 上查询与 process runner 相同的可处理 inbox 集合。
	hasInbox, err := repository.HasProcessableRedisUsageInbox(ctx, writeDB)
	// inbox 查询失败时保守返回错误，绝不猜测前台没有 backlog。
	if err != nil {
		return result, err
	}
	// 发现 pending/process_failed 时不启动任何聚合写事务。
	if hasInbox {
		result.DeferredForInbox = true
		return result, nil
	}

	// 只短暂读取轮转类型和 identity 内存 cursor。
	r.mu.Lock()
	// RunOnce 读取当前事务配额；失败不推进数据库 checkpoint，后台调用方可把配额轮转给其它 kind。
	kind := r.nextKind
	// identity cursor 只有 Identity kind 会使用。
	identityAfterID := r.identityAfterID
	// 当前分页轮默认沿用第一页成功时保存的通知代际。
	identityPassGeneration := r.identityScanGeneration
	// 从 ID 头部开始的新一轮扫描必须捕获此刻最新目标代际。
	if kind == UsageAggregationKindIdentity && identityAfterID == 0 {
		identityPassGeneration = r.identityTargetGeneration
	}
	// 事务开始前释放内存锁，不阻塞前台 notifier。
	r.mu.Unlock()
	// 结果记录本次实际选择的独立聚合类型。
	result.Kind = kind
	// 本轮 now 只读取一次，传给一个有界事务。
	now := r.now()
	// 短事务忽略 App cancel 以完成原子提交，但保留 caller deadline 并增加最大执行上限。
	transactionCtx, cancelTransaction := boundedUsageAggregationContext(ctx, usageAggregationTransactionTimeout)
	// 无论事务成功或失败都释放 deadline timer。
	defer cancelTransaction()

	// 根据公平轮转状态只调用一种 repository batch。
	switch kind {
	case UsageAggregationKindOverview:
		// Overview batch 只写旧 hourly/daily 和自己的 checkpoint。
		processed, runErr := repository.AggregateUsageOverviewStatsBatch(transactionCtx, writeDB, now)
		// RunOnce 自身不推进轮转；后台 Run 会在记录失败后把配额暂时交给其它 kind。
		if runErr != nil {
			return result, runErr
		}
		// event 数大于零才表示本次真正处理了数据。
		result.Processed = processed > 0
		// 成功事务后轮转到 Activity。
		r.advanceAfterSuccess(UsageAggregationKindOverview, 0, false, 0)
		// App 已取消时 Overview 原子事务已经完成，不再启动额外 header cursor 查询。
		if ctx.Err() != nil {
			return result, nil
		}
		// header gate 不属于已开始的聚合事务，必须响应 App cancel 并使用独立短上限。
		headerCtx, cancelHeader := context.WithTimeout(ctx, usageAggregationHeaderCursorTimeout)
		// 只有 Overview 成功提交后才尝试释放满足 cursor gate 的 header snapshots。
		headerRetryPending, err := r.flushReadyHeaderSnapshots(headerCtx, writeDB)
		// cursor 查询或非阻塞 appender 返回后立即释放 header deadline timer。
		cancelHeader()
		// cursor 或 appender 前置查询失败时交给当前 Overview kind 的错误退避。
		if err != nil {
			return result, err
		}
		// appender 拒绝时保留显式重试状态，后台 Run 不依赖下一次外部 wake。
		result.HeaderRetryPending = headerRetryPending
	case UsageAggregationKindActivity:
		// Activity batch 只写新表和自己的 checkpoint。
		processed, runErr := repository.AggregateUsageActivityStatsBatch(transactionCtx, writeDB, now)
		// RunOnce 自身不推进轮转；后台 Run 会先调度其它 kind，Activity checkpoint 保持不变。
		if runErr != nil {
			return result, runErr
		}
		// event 数大于零才表示本次真正处理了 Activity 数据。
		result.Processed = processed > 0
		// 成功事务后轮转到 Identity。
		r.advanceAfterSuccess(UsageAggregationKindActivity, 0, false, 0)
	case UsageAggregationKindIdentity:
		// Identity batch 只处理固定一页 identities，并维护各行既有 cursor。
		batch, runErr := repository.AggregateUsageIdentityStatsBatch(transactionCtx, writeDB, now, identityAfterID)
		// 失败时不改变内存 cursor，下一次重试同一页。
		if runErr != nil {
			return result, runErr
		}
		// 成功后保存下一页 cursor；一轮结束时同时判断扫描期间是否收到更新通知。
		rescanRequired := r.advanceAfterSuccess(UsageAggregationKindIdentity, batch.LastIdentityID, batch.ReachedEnd, identityPassGeneration)
		// 有实际更新、尚有下一页或代际已过期时都必须继续当前 wake。
		result.Processed = batch.ProcessedIdentities > 0 || !batch.ReachedEnd || rescanRequired
	default:
		// 非法内存状态不能猜测事务类型，保持错误可观察。
		return result, fmt.Errorf("unknown usage aggregation kind %q", kind)
	}
	// 返回本次唯一事务的处理结果。
	return result, nil
}

// advanceAfterSuccess 只在对应短事务成功提交后推进公平轮转状态。
func (r *UsageAggregationRunner) advanceAfterSuccess(kind UsageAggregationKind, lastIdentityID int64, identityReachedEnd bool, identityPassGeneration uint64) bool {
	// 更新 next kind 与 identity cursor 必须和 notifier 的共享状态互斥。
	r.mu.Lock()
	// 函数结束统一释放轻量内存锁。
	defer r.mu.Unlock()
	// 三类成功事务按固定顺序轮转。
	switch kind {
	case UsageAggregationKindOverview:
		// Overview 成功后下一事务固定为 Activity。
		r.nextKind = UsageAggregationKindActivity
	case UsageAggregationKindActivity:
		// Activity 成功后下一事务固定为 Identity。
		r.nextKind = UsageAggregationKindIdentity
	case UsageAggregationKindIdentity:
		// Identity 每次只占一个事务配额，随后把 writer 交还 Overview。
		r.nextKind = UsageAggregationKindOverview
		// 一轮结束后从最小 identity ID 开始下一轮。
		if identityReachedEnd {
			r.identityAfterID = 0
			// 尾页完成后记录本轮真正覆盖到的通知代际。
			r.identityScanGeneration = identityPassGeneration
			// 扫描期间出现新通知时必须保持当前 wake，下一次 Identity 从 ID 头部重扫。
			return identityPassGeneration < r.identityTargetGeneration
		} else {
			// 未到末尾时保存本批最后 ID，下一轮 Identity 从其后继续。
			r.identityAfterID = lastIdentityID
			// 后续页面必须沿用第一页捕获的代际，不能把中途通知误认为已经覆盖。
			r.identityScanGeneration = identityPassGeneration
		}
	}
	// Overview、Activity 和未结束的 Identity 页面都不需要额外重扫标记。
	return false
}

// advanceAfterBackgroundFailure 只改变公平轮转配额，不推进任何数据库或 identity cursor。
func (r *UsageAggregationRunner) advanceAfterBackgroundFailure(kind UsageAggregationKind) {
	// 非法或空 kind 表示错误发生在事务选择前，不能修改轮转状态。
	if kind == "" {
		return
	}
	// 状态修改与 notifier 共享同一把轻量锁。
	r.mu.Lock()
	// 函数结束统一释放锁。
	defer r.mu.Unlock()
	// 失败类型下一轮排到其它两个类型之后再次获得配额。
	switch kind {
	case UsageAggregationKindOverview:
		// Overview 失败后先允许 Activity 运行。
		r.nextKind = UsageAggregationKindActivity
	case UsageAggregationKindActivity:
		// Activity 失败后先允许 Identity 运行。
		r.nextKind = UsageAggregationKindIdentity
	case UsageAggregationKindIdentity:
		// Identity 失败后先允许 Overview 运行，identity cursor 保持原值供后续重试。
		r.nextKind = UsageAggregationKindOverview
	}
}

// flushReadyHeaderSnapshots 只投递 Overview cursor 已覆盖的内存 snapshots，并报告是否需要有界重试。
func (r *UsageAggregationRunner) flushReadyHeaderSnapshots(ctx context.Context, writeDB *gorm.DB) (bool, error) {
	// 没有 appender 时 header 功能未启用，聚合本身仍正常运行。
	if r.headerAppender == nil {
		return false, nil
	}
	// 没有 pending snapshot 时不需要读取 Overview cursor 或占用唯一数据库连接。
	if !r.hasPendingHeaderSnapshots() {
		return false, nil
	}
	// cursor 必须从已提交 checkpoint 读取，不能使用内存猜测值。
	overviewCursor, err := repository.UsageOverviewAggregationCursor(ctx, writeDB)
	// cursor 查询失败时保留全部 snapshots，供下一轮重试。
	if err != nil {
		return false, err
	}
	// 加锁保证 appender 接收期间同一 auth_index 不被 notifier 替换。
	r.mu.Lock()
	// 函数结束统一释放 snapshot map 锁。
	defer r.mu.Unlock()
	// keys 先收集满足 gate 的 auth_index，后续排序保证投递顺序稳定。
	keys := make([]string, 0, len(r.pendingHeaders))
	// 遍历所有等待 snapshot，只选择所需 ID 已被 Overview 覆盖的项。
	for authIndex, pending := range r.pendingHeaders {
		// 落后的 Overview cursor 不能提前释放该项。
		if pending.requiredOverviewEventID > overviewCursor {
			continue
		}
		// 满足 gate 的 key 进入本次投递集合。
		keys = append(keys, authIndex)
	}
	// 没有 ready snapshot 时不调用 quota appender。
	if len(keys) == 0 {
		return false, nil
	}
	// auth_index 排序使批次行为和 map 迭代随机性无关。
	sort.Strings(keys)
	// 按稳定 key 顺序复制 ready snapshots。
	snapshots := make([]quota.UsageHeaderSnapshot, 0, len(keys))
	// 每个 key 只追加 map 中的最新 snapshot。
	for _, authIndex := range keys {
		// snapshot 已在 notifier 阶段 clone，可直接传给非阻塞 appender。
		snapshots = append(snapshots, r.pendingHeaders[authIndex].snapshot)
	}
	// appender 队列满时返回 false，必须原样保留 map 等待后续重试。
	if !r.headerAppender.TryAppendUsageHeaderSnapshots(snapshots) {
		return true, nil
	}
	// appender 接受后才删除本次已经投递的 auth_index。
	for _, authIndex := range keys {
		// 删除只影响已经越过 Overview gate 且成功入队的 snapshot。
		delete(r.pendingHeaders, authIndex)
	}
	// 所有 ready snapshots 已成功移交 quota worker。
	return false, nil
}

func (r *UsageAggregationRunner) flushReadyHeaderSnapshotsOnShutdown(ctx context.Context) {
	// nil runner 或未配置 appender 时没有关机投递工作。
	if r == nil || r.headerAppender == nil {
		return
	}
	// 没有 pending snapshot 时无需读取 Overview cursor，避免关闭路径等待唯一数据库连接。
	if !r.hasPendingHeaderSnapshots() {
		return
	}
	// pending snapshot 的关闭投递是 best-effort，忽略 App cancel 但只等待固定短上限。
	flushCtx, cancelFlush := boundedUsageAggregationContext(ctx, usageAggregationShutdownFlushTimeout)
	// 函数退出时释放 shutdown deadline timer。
	defer cancelFlush()
	// 关机回读仍属于已提交聚合的收尾，固定 writer 可避免 reader 饱和拖过关闭上限。
	writeDB := r.db.Clauses(dbresolver.Write).Session(&gorm.Session{Context: flushCtx})
	// 关闭只尝试一次非阻塞 appender，不进入退避或启动新的聚合事务。
	retryPending, err := r.flushReadyHeaderSnapshots(flushCtx, writeDB)
	// 数据库读取失败时保留错误日志；usage 与已提交聚合结果不受影响。
	if err != nil {
		logrus.WithError(err).Warn("usage aggregation shutdown header flush failed")
		return
	}
	// appender 拒绝表示 quota worker 队列已满，关闭路径不再等待。
	if retryPending {
		logrus.Warn("usage aggregation shutdown header flush skipped because quota queue is full")
	}
}

// hasPendingHeaderSnapshots 只检查关闭路径是否需要访问数据库读取 Overview cursor。
func (r *UsageAggregationRunner) hasPendingHeaderSnapshots() bool {
	// nil runner 不持有任何等待快照。
	if r == nil {
		return false
	}
	// pendingHeaders 与前台 notifier 共享，读取长度也必须持有同一把锁。
	r.mu.Lock()
	// 长度读取完成立即释放轻量内存锁。
	defer r.mu.Unlock()
	// 非空 map 才需要执行 shutdown flush。
	return len(r.pendingHeaders) > 0
}

// boundedUsageAggregationContext 忽略 parent cancel，同时保留更早 deadline 并添加最大上限。
func boundedUsageAggregationContext(parent context.Context, maxDuration time.Duration) (context.Context, context.CancelFunc) {
	// 调用方都已规范 nil context；这里仍防御性使用 Background。
	if parent == nil {
		parent = context.Background()
	}
	// WithoutCancel 保留 context values，但让已开始短事务不随 App shutdown 回滚。
	base := context.WithoutCancel(parent)
	// 最大 deadline 防止单连接获取或 SQLite 调用无限等待。
	deadline := time.Now().Add(maxDuration)
	// caller 自己设置的更早 deadline 必须保留，不能被 WithoutCancel 静默删除。
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	// 返回可释放 timer 的有界 detached context。
	return context.WithDeadline(base, deadline)
}
