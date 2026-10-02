package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/pricingmetadata"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var (
	ErrPricingBusy                      = errors.New("pricing recalculation is running")
	ErrPricingChanged                   = errors.New("pricing configuration changed")
	ErrInvalidPricingRecalculationStart = errors.New("invalid pricing recalculation start")
)

// PricingRecalculationDependencies 显式提供重算需要的停稳许可、缓存和 App 生命周期。
// LifecycleCtx 由 App 管理；HTTP 请求 context 只用于受理，不控制已创建的后台任务。
type PricingRecalculationDependencies struct {
	LifecycleCtx context.Context
	Sync         *SyncService
	Aggregation  *poller.UsageAggregationRunner
	CostReadGate *CostReadGate
	Recent       *repository.UsageRecentEventCache
	Quota        *quota.Service
	Now          func() time.Time
}

// NewPricingServiceWithRecalculation 在同一价格服务上装配单任务协调，不建立第二套价格写锁或持久任务表。
// App 调用方必须传入自身生命周期及真实处理、聚合和缓存依赖；缺失时启动装配直接失败。
func NewPricingServiceWithRecalculation(db *gorm.DB, catalog *pricing.Catalog, opts PricingRecalculationDependencies, modelsFetcher ...ModelsFetcher) PricingProvider {
	if opts.LifecycleCtx == nil || opts.Sync == nil || opts.Aggregation == nil || opts.CostReadGate == nil || opts.Recent == nil || opts.Quota == nil {
		panic("pricing recalculation dependencies are required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &pricingService{db: db, catalog: requirePricingCatalog(catalog), metadataClient: pricingmetadata.NewClient(nil), recalculation: opts}
	if len(modelsFetcher) > 0 {
		s.modelsFetcher = modelsFetcher[0]
	}
	return s
}

// GetPricingRecalculationOptions 从真实热表与最近 30×24 小时的交集给出合法绝对小时，不按本地 HH:00 伪造边界。
// 配置修订在价格锁内读取；返回的选项仅供显示，Start 会在新的受理时刻重新验证。
func (s *pricingService) GetPricingRecalculationOptions(ctx context.Context) (servicedto.RecalculationOptions, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	hotEarliest, err := repository.LoadEarliestHotUsageHour(ctx, s.db)
	if err != nil {
		return servicedto.RecalculationOptions{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	now := timeutil.NormalizeStorageTime(s.recalculation.Now())
	revision, err := repository.LoadPricingConfigRevision(s.db.WithContext(ctx))
	if err != nil {
		return servicedto.RecalculationOptions{}, err
	}
	return pricingRecalculationOptions(now, hotEarliest, revision), nil
}

// StartPricingRecalculation 在配置锁内固定当前版本、S/T 和只读价格快照，再由 App 生命周期异步执行。
// 运行中重复请求直接取得现行任务，不检查旧修订或入队；受理后 HTTP 断开不会取消任务。
func (s *pricingService) StartPricingRecalculation(ctx context.Context, request servicedto.StartRecalculationRequest) (servicedto.StartRecalculationResponse, error) {
	s.mutationMu.Lock()
	if s.recalculationRunning {
		current := clonePricingRecalculationTask(s.recalculationTask)
		s.mutationMu.Unlock()
		return servicedto.StartRecalculationResponse{Started: false, Task: *current}, nil
	}
	s.mutationMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return servicedto.StartRecalculationResponse{}, err
	}
	hotEarliest, err := repository.LoadEarliestHotUsageHour(ctx, s.db)
	if err != nil {
		return servicedto.StartRecalculationResponse{}, err
	}
	var taskIDBytes [16]byte
	if _, err := rand.Read(taskIDBytes[:]); err != nil {
		return servicedto.StartRecalculationResponse{}, fmt.Errorf("create pricing recalculation task id: %w", err)
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationRunning {
		return servicedto.StartRecalculationResponse{Started: false, Task: *clonePricingRecalculationTask(s.recalculationTask)}, nil
	}
	if err := s.recalculation.LifecycleCtx.Err(); err != nil {
		return servicedto.StartRecalculationResponse{}, fmt.Errorf("pricing recalculation lifecycle ended: %w", err)
	}
	now := timeutil.NormalizeStorageTime(s.recalculation.Now())
	if err := validatePricingRecalculationStart(request, pricingRecalculationOptions(now, hotEarliest, 0), now); err != nil {
		return servicedto.StartRecalculationResponse{}, err
	}
	revision, err := repository.LoadPricingConfigRevision(s.db.WithContext(ctx))
	if err != nil {
		return servicedto.StartRecalculationResponse{}, err
	}
	if request.ConfigRevision != revision {
		return servicedto.StartRecalculationResponse{}, ErrPricingChanged
	}
	task := &servicedto.RecalculationTask{
		TaskID: hex.EncodeToString(taskIDBytes[:]), Status: servicedto.RecalculationRunning,
		Stage: servicedto.RecalculationPreparing, StartAt: request.StartAt.In(time.Local), EndAt: now,
		ConfigRevision: revision, UpdatedAt: now,
	}
	snapshot := s.catalog.Snapshot()
	s.recalculationTask = task
	s.recalculationRunning = true
	s.recalculationWG.Add(1)
	go s.runPricingRecalculation(task.TaskID, snapshot, task.StartAt, task.EndAt)
	return servicedto.StartRecalculationResponse{Started: true, Task: *clonePricingRecalculationTask(task)}, nil
}

// CurrentPricingRecalculation 只返回当前进程的任务副本；没有任务或重启后返回 nil。
func (s *pricingService) CurrentPricingRecalculation(ctx context.Context) (*servicedto.RecalculationTask, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	return clonePricingRecalculationTask(s.recalculationTask), nil
}

// WaitPricingRecalculation 供 App 取消生命周期后等待唯一 worker 退出，避免关库早于批次回滚和许可释放。
func (s *pricingService) WaitPricingRecalculation() {
	// 与受理的 WaitGroup.Add 共用配置锁，避免 App 关库与最后一刻的新任务交错。
	s.mutationMu.Lock()
	s.mutationMu.Unlock()
	s.recalculationWG.Wait()
}

// pricingRecalculationOptions 以真实 instant 求合法绝对小时，不把半小时时区的本地分钟归零。
func pricingRecalculationOptions(now time.Time, hotEarliest *time.Time, revision int64) servicedto.RecalculationOptions {
	options := servicedto.RecalculationOptions{Timezone: time.Local.String(), StepSeconds: 3600, MaxDays: 30, ConfigRevision: revision}
	if hotEarliest == nil {
		return options
	}
	// 最早事件所在的整个现有小时可重算；从事件本身的分钟向后取整会永久漏掉首个部分小时。
	lowerBound := now.Add(-30 * 24 * time.Hour)
	if hotEarliest.After(lowerBound) {
		lowerBound = *hotEarliest
	}
	earliest := lowerBound.Truncate(time.Hour)
	if earliest.Before(lowerBound) {
		earliest = earliest.Add(time.Hour)
	}
	latest := now.Truncate(time.Hour)
	if !latest.Before(now) {
		latest = latest.Add(-time.Hour)
	}
	if !earliest.After(latest) {
		localEarliest := earliest.In(time.Local)
		localLatest := latest.In(time.Local)
		options.EarliestStart = &localEarliest
		options.LatestStart = &localLatest
	}
	return options
}

// validatePricingRecalculationStart 在受理时刻核对实际小时、热表下界和修订确认之外的时间约束。
func validatePricingRecalculationStart(request servicedto.StartRecalculationRequest, options servicedto.RecalculationOptions, now time.Time) error {
	if request.ConfigRevision < 0 {
		return fmt.Errorf("%w: %w", ErrInvalidPricingRecalculationStart,
			&pricing.ValidationError{Path: "config_revision", Code: "invalid", Reason: "config_revision must be non-negative"})
	}
	start := request.StartAt
	if options.EarliestStart == nil || options.LatestStart == nil || start.IsZero() || !start.Equal(start.Truncate(time.Hour)) ||
		!start.Before(now) || start.Before(*options.EarliestStart) || start.After(*options.LatestStart) {
		return fmt.Errorf("%w: %w", ErrInvalidPricingRecalculationStart,
			&pricing.ValidationError{Path: "start_at", Code: "invalid", Reason: "start_at is outside the available hourly range"})
	}
	return nil
}

// runPricingRecalculation 只使用 App 生命周期；成功或失败都在缓存处理和许可释放后发布最终状态。
func (s *pricingService) runPricingRecalculation(taskID string, snapshot *pricing.Snapshot, start, end time.Time) {
	defer s.recalculationWG.Done()
	err := s.executePricingRecalculation(s.recalculation.LifecycleCtx, taskID, snapshot, start, end)
	if err != nil {
		logrus.WithError(err).WithField("task_id", taskID).Error("pricing recalculation failed")
	}
	s.mutationMu.Lock()
	if s.recalculationTask != nil && s.recalculationTask.TaskID == taskID {
		if err != nil {
			s.recalculationTask.Status = servicedto.RecalculationFailed
			s.recalculationTask.Error = &servicedto.RecalculationTaskError{Code: "recalculation_failed", Message: "Historical cost recalculation did not complete"}
		} else {
			s.recalculationTask.Status = servicedto.RecalculationCompleted
		}
		s.recalculationTask.UpdatedAt = s.recalculation.Now().In(time.Local)
		s.recalculationRunning = false
	}
	s.mutationMu.Unlock()
}

// executePricingRecalculation 先停稳三种许可，再固定 H 并受控追平；每页短事务提交后才增加处理数。
// 全部停稳之前失败只释放已经取得的许可，不能调用要求 recent 已排空的结束缓存处理。
func (s *pricingService) executePricingRecalculation(ctx context.Context, taskID string, snapshot *pricing.Snapshot, start, end time.Time) (runErr error) {
	resumeUsage, err := s.recalculation.Sync.PauseUsageWork(ctx)
	if err != nil {
		return err
	}
	resumeAggregation, err := s.recalculation.Aggregation.Pause(ctx)
	if err != nil {
		resumeUsage()
		return err
	}
	resumeReads, err := s.recalculation.CostReadGate.BlockAndDrain(ctx)
	if err != nil {
		resumeAggregation()
		resumeUsage()
		return err
	}
	defer func() {
		s.setPricingRecalculationStage(taskID, servicedto.RecalculationFinalizing)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		finishErr := FinishPricingRecalculation(cleanupCtx, s.db, s.recalculation.Recent, s.recalculation.Quota, func() {
			resumeReads()
			resumeAggregation()
			resumeUsage()
		})
		runErr = errors.Join(runErr, finishErr)
	}()

	maxEventID, err := repository.LoadUsageAggregationTargetEventID(ctx, s.db)
	if err != nil {
		return err
	}
	scope := repository.UsageCostRecalculationScope{Start: start, End: end, MaxEventID: maxEventID}
	total, err := repository.CountUsageCostRecalculationEvents(ctx, s.db, scope)
	if err != nil {
		return err
	}
	s.setPricingRecalculationTotal(taskID, total)
	// 空目标不重放任何无关旧事件，也不推动原 Overview 水位。
	if total == 0 {
		return nil
	}
	if err := s.recalculation.Aggregation.CatchUpOverviewTo(ctx, maxEventID); err != nil {
		return err
	}
	s.setPricingRecalculationStage(taskID, servicedto.RecalculationEvents)
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	for cursor := int64(0); ; {
		events, next, done, err := repository.LoadUsageCostRecalculationPage(ctx, s.db, scope, cursor)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			if err := repository.ApplyUsageCostRecalculationBatch(ctx, s.db, scope, events, resolver); err != nil {
				return err
			}
			s.addPricingRecalculationProcessed(taskID, int64(len(events)))
		}
		if done {
			break
		}
		cursor = next
	}
	s.setPricingRecalculationStage(taskID, servicedto.RecalculationUpdatingStats)
	return repository.FinalizeUsageCostRecalculationStats(ctx, s.db, s.db, scope)
}

// setPricingRecalculationTotal 在目标集合固定后公布真实分母，追平完成前仍保持准备阶段。
func (s *pricingService) setPricingRecalculationTotal(taskID string, total int64) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationTask != nil && s.recalculationTask.TaskID == taskID {
		s.recalculationTask.TotalCount = &total
		s.recalculationTask.UpdatedAt = s.recalculation.Now().In(time.Local)
	}
}

// addPricingRecalculationProcessed 只在事件、小时、日事务一起提交后增加进度，回滚页不计数。
func (s *pricingService) addPricingRecalculationProcessed(taskID string, count int64) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationTask != nil && s.recalculationTask.TaskID == taskID {
		s.recalculationTask.ProcessedCount += count
		s.recalculationTask.UpdatedAt = s.recalculation.Now().In(time.Local)
	}
}

// setPricingRecalculationStage 发布汇总覆盖和结束处理阶段，不提前声明任务完成。
func (s *pricingService) setPricingRecalculationStage(taskID string, stage servicedto.RecalculationStage) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationTask != nil && s.recalculationTask.TaskID == taskID {
		s.recalculationTask.Stage = stage
		s.recalculationTask.UpdatedAt = s.recalculation.Now().In(time.Local)
	}
}

// clonePricingRecalculationTask 隔离 API 读者对可空进度和错误字段的修改。
func clonePricingRecalculationTask(task *servicedto.RecalculationTask) *servicedto.RecalculationTask {
	if task == nil {
		return nil
	}
	copy := *task
	if task.TotalCount != nil {
		total := *task.TotalCount
		copy.TotalCount = &total
	}
	if task.Error != nil {
		failure := *task.Error
		copy.Error = &failure
	}
	return &copy
}
