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
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ServiceOptions struct {
	RefreshWorkerLimit  int
	AutoRefreshInterval time.Duration
}

type Service struct {
	db       *gorm.DB
	registry ProviderRegistry

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
	// autoRefreshInterval 控制后台 runner 的 tick 周期。
	autoRefreshInterval time.Duration
	// autoRefreshActiveTTL 控制前端心跳失效前可继续自动刷新的时间窗。
	autoRefreshActiveTTL time.Duration
	// activeMu 保护 lastActiveStatusAt，避免 status 心跳和后台 runner 并发读写。
	activeMu sync.Mutex
	// lastActiveStatusAt 只保存在内存中，用来判断后台页面是否仍然活跃。
	lastActiveStatusAt time.Time
	// autoRefreshMu 保护 autoRefreshRunning，避免多个 tick 同时启动扫描。
	autoRefreshMu sync.Mutex
	// autoRefreshRunning 表示上一轮自动刷新还有 queued/running 任务未完全结束。
	autoRefreshRunning bool
	// lastAutoRefreshAttemptAt 记录最近一次尝试启动整轮自动刷新的时间，用于扫描失败退避。
	lastAutoRefreshAttemptAt time.Time
	// lastAutoRefreshRoundAt 记录上次启动整轮 Auth Files 自动刷新入队的内存时间。
	lastAutoRefreshRoundAt time.Time
	// refreshLifecycleMu 保护 refreshClosing 和 refreshWG.Add，避免关闭等待期间继续登记后台 goroutine。
	refreshLifecycleMu sync.Mutex
	// refreshClosing 表示 App 正在关闭 quota 后台任务，后续心跳/刷新请求不能再派生新 goroutine。
	refreshClosing bool
	// refreshWG 跟踪 service 派生的 dispatcher/worker/heartbeat goroutine，App 关闭 DB 前会等待它们退出。
	refreshWG sync.WaitGroup
}

type CheckRequest struct {
	AuthIndex string `json:"auth_index"`
}

type CheckResponse struct {
	ID                                  string     `json:"id"`
	Quota                               []QuotaRow `json:"quota"`
	RateLimitResetCreditsAvailableCount *int       `json:"rateLimitResetCreditsAvailableCount,omitempty"`
}

func NewService(db *gorm.DB, caller ManagementAPICaller) *Service {
	return NewServiceWithOptions(db, caller, ServiceOptions{})
}

func NewServiceWithOptions(db *gorm.DB, caller ManagementAPICaller, options ServiceOptions) *Service {
	return NewServiceWithRegistryAndOptions(db, NewDefaultProviderRegistry(caller, DefaultProviderConfigs()), options)
}

func NewServiceWithRegistry(db *gorm.DB, registry ProviderRegistry) *Service {
	return NewServiceWithRegistryAndOptions(db, registry, ServiceOptions{})
}

func NewServiceWithRegistryAndOptions(db *gorm.DB, registry ProviderRegistry, options ServiceOptions) *Service {
	workerLimit := options.RefreshWorkerLimit
	if workerLimit <= 0 {
		workerLimit = RefreshWorkerLimit
	}
	if workerLimit > 100 {
		workerLimit = 100
	}
	autoRefreshInterval := options.AutoRefreshInterval
	if autoRefreshInterval <= 0 {
		autoRefreshInterval = AutoRefreshInterval
	}
	refreshContext, refreshCancel := context.WithCancel(context.Background())
	return &Service{
		db:                   db,
		registry:             registry,
		refreshTasks:         make(map[string]*RefreshTaskRecord),
		resetInFlight:        make(map[string]struct{}),
		refreshWorkerTokens:  make(chan struct{}, workerLimit),
		refreshTaskTTL:       RefreshTransientTaskTTL,
		refreshCooldown:      time.Sleep,
		refreshContext:       refreshContext,
		refreshCancel:        refreshCancel,
		autoRefreshInterval:  autoRefreshInterval,
		autoRefreshActiveTTL: AutoRefreshActiveTTL,
	}
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

func (s *Service) RecordActiveStatus(at time.Time) {
	// nil service 直接返回，方便 API 层在测试或可选 provider 场景安全调用。
	if s == nil {
		return
	}
	// 心跳时间先归一化，后续 active TTL 和自动刷新间隔判断共用同一时间口径。
	normalizedAt := timeutil.NormalizeStorageTime(at)
	// 写入活跃时间前加锁，避免并发心跳和自动刷新判断产生数据竞争。
	s.activeMu.Lock()
	// wasActive 在更新时间前计算，用来判断这次心跳是否把后台页面从 inactive 拉回 active。
	wasActive := !s.lastActiveStatusAt.IsZero() && normalizedAt.Sub(s.lastActiveStatusAt) <= s.autoRefreshActiveTTL
	// 活跃时间按项目存储时间口径归一化，但只保存在内存中，不写数据库。
	s.lastActiveStatusAt = normalizedAt
	// 立即解锁，避免异步唤醒自动刷新时持有 activeMu 造成锁嵌套。
	s.activeMu.Unlock()
	// 只有从 inactive 变 active 的第一跳才尝试唤醒自动刷新，30s 续约心跳不会重复触发整轮扫描。
	if !wasActive {
		// 唤醒动作放到 goroutine，避免 status/active 接口被数据库扫描或 provider 队列阻塞。
		refreshContext := s.refreshContextSnapshot()
		s.startRefreshGoroutine(func() {
			// RunAutoRefresh 内部还会检查上次整轮刷新时间，刚刚刷过时会直接跳过。
			if err := s.RunAutoRefresh(refreshContext); err != nil {
				logrus.Errorf("quota auto refresh failed after active heartbeat: %v", err)
			}
		})
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
	s.refreshWG.Wait()
}

func (s *Service) HasRecentActiveStatus(now time.Time) bool {
	// nil service 没有后台活跃状态，自动刷新应当跳过。
	if s == nil {
		return false
	}
	// 读取活跃时间前加锁，和 RecordActiveStatus 的写入锁保持一致。
	s.activeMu.Lock()
	// defer 解锁，保证所有返回路径都会释放锁。
	defer s.activeMu.Unlock()
	// 从未收到前端心跳时视为无人查看后台页面。
	if s.lastActiveStatusAt.IsZero() {
		return false
	}
	// 当前时间同样归一化后再比较，保持 TTL 判断口径一致。
	return timeutil.NormalizeStorageTime(now).Sub(s.lastActiveStatusAt) <= s.autoRefreshActiveTTL
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
	response := CheckResponse{
		ID:    authIndex,
		Quota: NormalizeQuotaRows(providerOutput),
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
