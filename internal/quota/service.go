package quota

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ServiceOptions struct {
	RefreshWorkerLimit               int
	UsageHeaderSnapshotFlushInterval time.Duration
	// CodexQuotaHistoryFlushInterval 覆盖独立历史 runner 固定批次边界前的十秒等待，主要供定向测试缩短等待。
	CodexQuotaHistoryFlushInterval time.Duration
	// CodexQuotaHistoryQueueSize 分别覆盖 Header 与可信主动查询两条有界队列容量，非正值使用生产默认值。
	CodexQuotaHistoryQueueSize int
	PricingCatalog             *pricing.Catalog
}

type Service struct {
	db       *gorm.DB
	registry ProviderRegistry
	pricing  *pricing.Catalog

	refreshMu    sync.Mutex
	refreshTasks map[string]*RefreshTaskRecord
	// resetInFlight 按 auth_index 记录正在消费的 reset credit，避免并发重复扣减官方次数。
	resetMu       sync.Mutex
	resetInFlight map[string]struct{}
	// inspectionCompletedAt 只记录用户手动启动巡检后，该巡检轮次完成的时间。
	inspectionCompletedAt       time.Time
	inspectionRoundActive       bool
	inspectionRoundAuthIndexSet map[string]struct{}
	refreshWorkerTokens         chan struct{}
	refreshTaskTTL              time.Duration
	refreshCooldown             func(time.Duration)
	refreshContext              context.Context
	refreshCancel               context.CancelFunc
	// autoRefreshMu 保护 autoRefreshRunning，避免多个 tick 同时启动扫描。
	autoRefreshMu sync.Mutex
	// autoRefreshRunning 表示上一轮自动刷新还有 queued/running 任务未完全结束。
	autoRefreshRunning bool
	// autoRefreshSettingsChanged 用于设置保存后唤醒调度循环，使长周期配置变更不必等到旧触发时间。
	autoRefreshSettingsChanged chan struct{}
	// autoRefreshNow 和 autoRefreshDelay 仅用于调度器测试固定时间和跳过真实等待。
	autoRefreshNow   func() time.Time
	autoRefreshDelay func(context.Context, time.Duration) bool
	// lastAutoRefreshAttemptAt 记录最近一次尝试启动整轮自动刷新的时间，用于扫描失败退避。
	lastAutoRefreshAttemptAt time.Time
	// lastAutoRefreshRoundAt 记录上次启动整轮 Auth Files 自动刷新入队的内存时间。
	lastAutoRefreshRoundAt time.Time
	// refreshLifecycleMu 保护 refreshClosing 和 refreshWG.Add，避免关闭等待期间继续登记后台 goroutine。
	refreshLifecycleMu sync.Mutex
	// refreshClosing 表示 App 正在关闭 quota 后台任务，后续刷新请求不能再派生新 goroutine。
	refreshClosing bool
	// refreshWG 跟踪 service 派生的 dispatcher/worker/scheduler goroutine，App 关闭 DB 前会等待它们退出。
	refreshWG sync.WaitGroup

	// usageHeaderPending 按 auth_index 只保存一分钟窗口内最新的不可变快照指针。
	usageHeaderPending       map[string]*UsageHeaderSnapshot
	usageHeaderWake          chan struct{}
	usageHeaderStopCh        chan struct{}
	usageHeaderDoneCh        chan struct{}
	usageHeaderFlushInterval time.Duration
	usageHeaderNewTimer      func(time.Duration) (<-chan time.Time, func())
	usageHeaderMu            sync.Mutex
	usageHeaderClosing       bool
	usageHeaderCloseOnce     sync.Once

	// codexQuotaHistoryHeaderQueue 有界保存 Header 快照指针，生产者永不等待。
	codexQuotaHistoryHeaderQueue chan codexQuotaHistoryInput
	// codexQuotaHistoryHeaderWake 只通知 runner“Header 队列已有数据”；容量为一即可合并重复唤醒。
	codexQuotaHistoryHeaderWake chan struct{}
	// codexQuotaHistoryTrustedQueue 独立保存低频可信主动查询 observation，不与 Header 共用 writer 批次。
	codexQuotaHistoryTrustedQueue chan codexQuotaHistoryInput
	// codexQuotaHistoryTrustedWake 让可信来源跳过 Header 十秒窗口并立即触发 runner。
	codexQuotaHistoryTrustedWake chan struct{}
	// codexQuotaHistoryStopCh 只表达 runner 停止；队列不关闭以避免并发发送 panic。
	codexQuotaHistoryStopCh chan struct{}
	// codexQuotaHistoryDoneCh 在 shutdown best-effort flush 完成后关闭。
	codexQuotaHistoryDoneCh chan struct{}
	// codexQuotaHistoryFlushInterval 是首条数据入队后、runner 固定本批队列数量前的等待时长。
	codexQuotaHistoryFlushInterval time.Duration
	// codexQuotaHistoryNewTimer 创建一次性窗口 timer，测试可替换但生产默认使用 time.Timer。
	codexQuotaHistoryNewTimer func(time.Duration) (<-chan time.Time, func())
	// codexQuotaHistoryWrite 是独立 repository writer；不进入 usage/inbox 事务。
	codexQuotaHistoryWrite codexQuotaHistoryWriter
	// codexQuotaHistoryLoad 在缓存首次命中或写失败失效后从 writer 恢复当前周期尾段。
	codexQuotaHistoryLoad codexQuotaHistoryLoader
	// codexQuotaHistoryListIdentities 批量确认 Header 来源属于活跃 Codex Auth File。
	codexQuotaHistoryListIdentities codexQuotaHistoryIdentityLister
	// codexQuotaHistoryMu 同时保护 closing 检查和非阻塞发送，避免 stop/send 竞态。
	codexQuotaHistoryMu sync.Mutex
	// codexQuotaHistoryClosing 表示 runner 已停止接收任何新 history 候选。
	codexQuotaHistoryClosing bool
	// codexQuotaHistoryCloseOnce 保证重复 StopRefreshTasks 只关闭一次 stop channel。
	codexQuotaHistoryCloseOnce sync.Once
}

type CheckRequest struct {
	AuthIndex string `json:"auth_index"`
	// Source 决定运行期额度历史的队列分流与可信校准权限，不进入数据库或对外响应。
	Source RefreshSource `json:"-"`
}

type CheckResponse struct {
	ID                                  string            `json:"id"`
	Quota                               []QuotaRow        `json:"quota"`
	Subscription                        *SubscriptionInfo `json:"subscription,omitempty"`
	RateLimitResetCreditsAvailableCount *int              `json:"rateLimitResetCreditsAvailableCount,omitempty"`
}

func NewService(db *gorm.DB, caller ManagementAPICaller, pricingCatalog *pricing.Catalog) *Service {
	return NewServiceWithOptions(db, caller, ServiceOptions{PricingCatalog: pricingCatalog})
}

func NewServiceWithOptions(db *gorm.DB, caller ManagementAPICaller, options ServiceOptions) *Service {
	return NewServiceWithRegistryAndOptions(db, NewDefaultProviderRegistry(caller, DefaultProviderConfigs()), options)
}

func NewServiceWithRegistry(db *gorm.DB, registry ProviderRegistry, pricingCatalog *pricing.Catalog) *Service {
	return NewServiceWithRegistryAndOptions(db, registry, ServiceOptions{PricingCatalog: pricingCatalog})
}

func NewServiceWithRegistryAndOptions(db *gorm.DB, registry ProviderRegistry, options ServiceOptions) *Service {
	workerLimit := options.RefreshWorkerLimit
	if workerLimit <= 0 {
		workerLimit = RefreshWorkerLimit
	}
	if workerLimit > 100 {
		workerLimit = 100
	}
	usageHeaderFlushInterval := options.UsageHeaderSnapshotFlushInterval
	if usageHeaderFlushInterval <= 0 {
		usageHeaderFlushInterval = usageHeaderSnapshotFlushInterval
	}
	// 独立 history runner 默认每 10 秒合并一次；非正测试覆盖不改变生产语义。
	codexHistoryFlushInterval := options.CodexQuotaHistoryFlushInterval
	if codexHistoryFlushInterval <= 0 {
		codexHistoryFlushInterval = codexQuotaHistoryFlushInterval
	}
	// 队列容量只限制内存和丢弃边界，不改变 cache latest-map 的接收能力。
	codexHistoryQueueSize := options.CodexQuotaHistoryQueueSize
	if codexHistoryQueueSize <= 0 {
		codexHistoryQueueSize = codexQuotaHistoryQueueSize
	}
	refreshContext, refreshCancel := context.WithCancel(context.Background())
	pricingCatalog := options.PricingCatalog
	if pricingCatalog == nil {
		panic("pricing catalog is required")
	}
	service := &Service{
		db:                              db,
		registry:                        registry,
		pricing:                         pricingCatalog,
		refreshTasks:                    make(map[string]*RefreshTaskRecord),
		resetInFlight:                   make(map[string]struct{}),
		refreshWorkerTokens:             make(chan struct{}, workerLimit),
		refreshTaskTTL:                  RefreshTransientTaskTTL,
		refreshCooldown:                 time.Sleep,
		refreshContext:                  refreshContext,
		refreshCancel:                   refreshCancel,
		autoRefreshSettingsChanged:      make(chan struct{}, 1),
		usageHeaderPending:              make(map[string]*UsageHeaderSnapshot, usageHeaderPendingIdentityLimit),
		usageHeaderWake:                 make(chan struct{}, 1),
		usageHeaderStopCh:               make(chan struct{}),
		usageHeaderDoneCh:               make(chan struct{}),
		usageHeaderFlushInterval:        usageHeaderFlushInterval,
		usageHeaderNewTimer:             newUsageHeaderTimer,
		codexQuotaHistoryHeaderQueue:    make(chan codexQuotaHistoryInput, codexHistoryQueueSize),
		codexQuotaHistoryHeaderWake:     make(chan struct{}, 1),
		codexQuotaHistoryTrustedQueue:   make(chan codexQuotaHistoryInput, codexHistoryQueueSize),
		codexQuotaHistoryTrustedWake:    make(chan struct{}, 1),
		codexQuotaHistoryStopCh:         make(chan struct{}),
		codexQuotaHistoryDoneCh:         make(chan struct{}),
		codexQuotaHistoryFlushInterval:  codexHistoryFlushInterval,
		codexQuotaHistoryNewTimer:       newCodexQuotaHistoryTimer,
		codexQuotaHistoryWrite:          repository.WriteCodexMainQuotaObservations,
		codexQuotaHistoryLoad:           repository.LoadLatestCodexQuotaHistoryState,
		codexQuotaHistoryListIdentities: repository.ListActiveAuthFileUsageIdentitiesByAuthIndexes,
	}
	go service.runUsageHeaderSnapshotWorker()
	// history 拥有独立队列、timer 和失败状态，不能复用一分钟 cache worker 的 pending map。
	go service.runCodexQuotaHistoryRunner()
	return service
}

func (s *Service) SetRefreshContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Service 自己持有一层 cancel，App.Close 即使没有外部 cancel 也能停止正在等待的刷新任务。
	nextContext, nextCancel := context.WithCancel(ctx)
	s.refreshLifecycleMu.Lock()
	previousCancel := s.refreshCancel
	if s.refreshClosing {
		s.refreshLifecycleMu.Unlock()
		nextCancel()
		return
	}
	s.refreshContext = nextContext
	s.refreshCancel = nextCancel
	s.refreshLifecycleMu.Unlock()
	if previousCancel != nil {
		// 替换父 context 时取消旧租约，避免旧 worker 继续挂在不可关闭的 context 上。
		previousCancel()
	}
}

func (s *Service) startRefreshGoroutine(fn func()) bool {
	if s == nil {
		return false
	}
	s.refreshLifecycleMu.Lock()
	defer s.refreshLifecycleMu.Unlock()
	if s.refreshClosing {
		return false
	}
	s.refreshWG.Add(1)
	go func() {
		defer s.refreshWG.Done()
		fn()
	}()
	return true
}

func (s *Service) WaitRefreshTasks() {
	if s == nil {
		return
	}
	s.refreshWG.Wait()
}

func (s *Service) refreshContextSnapshot() context.Context {
	if s == nil {
		return context.Background()
	}
	s.refreshLifecycleMu.Lock()
	defer s.refreshLifecycleMu.Unlock()
	if s.refreshContext == nil {
		return context.Background()
	}
	return s.refreshContext
}

func (s *Service) StopRefreshTasks() {
	if s == nil {
		return
	}
	s.refreshLifecycleMu.Lock()
	// 先封住新的后台 goroutine 登记，再等待已登记任务退出，避免 WaitGroup Add/Wait 并发。
	s.refreshClosing = true
	cancel := s.refreshCancel
	s.refreshLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.stopUsageHeaderSnapshotWorker()
	// 停止新投递并等待最多两秒的 history best-effort flush 后，调用方才能关闭数据库。
	s.stopCodexQuotaHistoryRunner()
	s.refreshWG.Wait()
}

func (s *Service) Check(ctx context.Context, request CheckRequest) (CheckResponse, error) {
	// 单条查询以 auth_index 为唯一入口，前端不需要知道具体 provider 的 API 细节。
	authIndex := strings.TrimSpace(request.AuthIndex)
	if authIndex == "" {
		return CheckResponse{}, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}
	// 只允许 auth files 身份查询限额，AI provider 身份不进入 provider 调用链路。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckResponse{}, fmt.Errorf("%w: %s", ErrNotFound, authIndex)
		}
		return CheckResponse{}, err
	}
	// 按相邻项目规则先匹配 provider 再匹配 type，解析出实际要调用的 quota handler。
	_, handler, ok := s.resolveQuotaHandlerForIdentity(identity)
	if !ok {
		return CheckResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeIdentityType(identity.Provider))
	}
	// provider 返回各自原始结构后，再统一转换为前端可复用的 quota rows。
	providerOutput, err := handler.Check(ctx, ProviderInput{Identity: identity})
	if err != nil {
		return CheckResponse{}, err
	}
	// 主动查询只从原始 CodexResult 提取 Primary/Secondary；Review/Additional 从结构上不参与遍历。
	if usageHeaderIdentityIsCodex(identity) {
		observations := BuildCodexMainQuotaObservations(authIndex, providerOutput, time.Now())
		if len(observations) > 0 && !s.tryAppendCodexQuotaHistoryObservations(observations, request.Source) {
			// history 是 best-effort 统计链路，队列满或 shutdown 不能改变手动/定时/巡检刷新结果。
			logrus.WithFields(logrus.Fields{
				"auth_index": authIndex,
				"source":     refreshSourceLogValue(request.Source),
			}).Warn("codex quota history active observation append skipped")
		}
	}
	response := CheckResponse{
		ID:           authIndex,
		Quota:        NormalizeQuotaRows(providerOutput),
		Subscription: NormalizeSubscription(providerOutput),
	}
	// 实时结果没有套餐时仅允许回退当前 Identity metadata；现在只有 Codex 具备该来源。
	if response.Subscription == nil {
		response.Subscription = ResolveIdentitySubscription(identity)
	}
	// reset 次数跟随官方刷新结果写入同一份限额缓存，前端只展示缓存里的官方值。
	if count, ok := rateLimitResetCreditsAvailableCount(providerOutput); ok {
		response.RateLimitResetCreditsAvailableCount = count
	}
	return response, nil
}

func (s *Service) resolveQuotaHandler(provider string, identityType string) (string, ProviderHandler, bool) {
	for _, candidate := range resolveQuotaIdentityTypes(provider, identityType) {
		if handler, ok := s.registry.Provider(candidate); ok {
			return candidate, handler, true
		}
	}
	return "", nil, false
}

func (s *Service) resolveQuotaHandlerForIdentity(identity entities.UsageIdentity) (string, ProviderHandler, bool) {
	return s.resolveQuotaHandler(identity.Provider, identity.Type)
}

func resolveQuotaIdentityTypes(provider string, identityType string) []string {
	candidates := make([]string, 0, 2)
	for _, value := range []string{provider, identityType} {
		normalized := normalizeIdentityType(value)
		if normalized == "" || slices.Contains(candidates, normalized) {
			continue
		}
		candidates = append(candidates, normalized)
	}
	return candidates
}

func rateLimitResetCreditsAvailableCount(output ProviderOutput) (*int, bool) {
	// 目前只有 Codex 返回 reset credit；其它 provider 保持字段缺省，避免误显示 reset 按钮。
	switch result := output.Result.(type) {
	case CodexResult:
		if result.Usage == nil || result.Usage.RateLimitResetCredits == nil || result.Usage.RateLimitResetCredits.AvailableCount == nil {
			return nil, false
		}
		return result.Usage.RateLimitResetCredits.AvailableCount, true
	case *CodexResult:
		if result == nil || result.Usage == nil || result.Usage.RateLimitResetCredits == nil || result.Usage.RateLimitResetCredits.AvailableCount == nil {
			return nil, false
		}
		return result.Usage.RateLimitResetCredits.AvailableCount, true
	default:
		return nil, false
	}
}
