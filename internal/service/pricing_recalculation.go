package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
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

const pricingRecalculationPendingKey = "pricing.recalculation.pending"

type pricingRecalculationMarker struct {
	Task  servicedto.RecalculationTask            `json:"task"`
	Scope *repository.UsageCostRecalculationScope `json:"scope,omitempty"`
	Count *int64                                  `json:"count,omitempty"`
}

func loadPricingRecalculationMarker(ctx context.Context, db *gorm.DB) (*pricingRecalculationMarker, error) {
	setting, found, err := repository.GetAppSetting(ctx, db, pricingRecalculationPendingKey)
	if err != nil || !found {
		return nil, err
	}
	if setting.Value == nil {
		return nil, fmt.Errorf("invalid recalculation interruption marker")
	}
	var marker pricingRecalculationMarker
	if err := json.Unmarshal([]byte(*setting.Value), &marker); err != nil {
		return nil, fmt.Errorf("invalid recalculation interruption marker: %w", err)
	}
	if marker.Task.StartAt.IsZero() || marker.Task.EndAt.IsZero() {
		return nil, fmt.Errorf("invalid recalculation interruption marker range")
	}
	return &marker, nil
}

func savePricingRecalculationMarker(ctx context.Context, db *gorm.DB, marker pricingRecalculationMarker) error {
	value, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	serialized := string(value)
	_, err = repository.UpsertAppSetting(ctx, db, entities.AppSetting{SettingKey: pricingRecalculationPendingKey, ValueType: entities.AppSettingValueTypeJSON, Value: &serialized})
	return err
}

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

// GetPricingRecalculationOptions exposes the earliest retained hour across hot and archive.
// 配置修订在价格锁内读取；返回的选项仅供显示，Start 会在新的受理时刻重新验证。
func (s *pricingService) GetPricingRecalculationOptions(ctx context.Context) (servicedto.RecalculationOptions, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	retainedEarliest, err := repository.LoadEarliestRetainedUsageHour(ctx, s.db)
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
	marker, err := loadPricingRecalculationMarker(ctx, s.db)
	if err != nil {
		return servicedto.RecalculationOptions{}, err
	}
	retainedEarliest = includeInterruptedStart(retainedEarliest, marker)
	return pricingRecalculationOptions(now, retainedEarliest, revision), nil
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
	retainedEarliest, err := repository.LoadEarliestRetainedUsageHour(ctx, s.db)
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
	previous, err := loadPricingRecalculationMarker(ctx, s.db)
	if err != nil {
		return servicedto.StartRecalculationResponse{}, err
	}
	retainedEarliest = includeInterruptedStart(retainedEarliest, previous)
	if err := validatePricingRecalculationStart(request, pricingRecalculationOptions(now, retainedEarliest, 0), now); err != nil {
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
	if previous != nil && !request.StartAt.Equal(previous.Task.StartAt) {
		return servicedto.StartRecalculationResponse{}, fmt.Errorf("%w: %w", ErrInvalidPricingRecalculationStart,
			&pricing.ValidationError{Path: "start_at", Code: "interrupted_range", Reason: "retry the interrupted start time"})
	}
	marker := pricingRecalculationMarker{Task: *task}
	if previous != nil {
		marker.Scope, marker.Count = previous.Scope, previous.Count
	}
	if err := savePricingRecalculationMarker(ctx, s.db, marker); err != nil {
		return servicedto.StartRecalculationResponse{}, fmt.Errorf("save recalculation interruption marker: %w", err)
	}
	s.recalculationTask = task
	s.recalculationRunning = true
	s.recalculationWG.Add(1)
	go s.runPricingRecalculation(task.TaskID, snapshot, task.StartAt, task.EndAt)
	return servicedto.StartRecalculationResponse{Started: true, Task: *clonePricingRecalculationTask(task)}, nil
}

// A retry must remain selectable even if retention removed events before its
// original start. The persisted scope and count are checked before any writes.
func includeInterruptedStart(earliest *time.Time, marker *pricingRecalculationMarker) *time.Time {
	if marker != nil && (earliest == nil || marker.Task.StartAt.Before(*earliest)) {
		start := marker.Task.StartAt
		return &start
	}
	return earliest
}

// CurrentPricingRecalculation returns the live task or a persisted interruption.
func (s *pricingService) CurrentPricingRecalculation(ctx context.Context) (*servicedto.RecalculationTask, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationTask != nil {
		return clonePricingRecalculationTask(s.recalculationTask), nil
	}
	marker, err := loadPricingRecalculationMarker(ctx, s.db)
	if err != nil || marker == nil {
		return nil, err
	}
	task := marker.Task
	task.Status = servicedto.RecalculationFailed
	task.Stage = servicedto.RecalculationFinalizing
	task.Error = &servicedto.RecalculationTaskError{Code: "recalculation_interrupted", Message: "Historical cost recalculation was interrupted; rerun the same range to finish"}
	return &task, nil
}

// AcknowledgeInterruptedPricingRecalculation explicitly abandons recovery of a
// partially completed run. A task ID prevents dismissing a newer interruption.
func (s *pricingService) AcknowledgeInterruptedPricingRecalculation(ctx context.Context, taskID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationRunning {
		return ErrPricingBusy
	}
	marker, err := loadPricingRecalculationMarker(ctx, s.db)
	if err != nil {
		return err
	}
	if marker == nil || taskID == "" || marker.Task.TaskID != taskID {
		return ErrPricingChanged
	}
	if err := s.db.WithContext(ctx).Where("setting_key = ?", pricingRecalculationPendingKey).Delete(&entities.AppSetting{}).Error; err != nil {
		return err
	}
	s.recalculationTask = nil
	return nil
}

// WaitPricingRecalculation 供 App 取消生命周期后等待唯一 worker 退出，避免关库早于批次回滚和许可释放。
func (s *pricingService) WaitPricingRecalculation() {
	// 与受理的 WaitGroup.Add 共用配置锁，避免 App 关库与最后一刻的新任务交错。
	s.mutationMu.Lock()
	s.mutationMu.Unlock()
	s.recalculationWG.Wait()
}

// pricingRecalculationOptions 以真实 instant 求合法绝对小时，不把半小时时区的本地分钟归零。
func pricingRecalculationOptions(now time.Time, retainedEarliest *time.Time, revision int64) servicedto.RecalculationOptions {
	options := servicedto.RecalculationOptions{Timezone: time.Local.String(), StepSeconds: 3600, MaxDays: 0, ConfigRevision: revision}
	if retainedEarliest == nil {
		return options
	}
	// 最早事件所在的整个现有小时可重算；从事件本身的分钟向后取整会永久漏掉首个部分小时。
	earliest := retainedEarliest.Truncate(time.Hour)
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

// validatePricingRecalculationStart checks alignment and the retained time range.
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
	if err == nil {
		// A crash before this deletion leaves a visible, safely repeatable marker.
		err = s.db.WithContext(context.Background()).Where("setting_key = ?", pricingRecalculationPendingKey).Delete(&entities.AppSetting{}).Error
	}
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
	var maxArchiveID int64
	if err := s.db.WithContext(ctx).Table("usage_events_archive").Select("COALESCE(MAX(id), 0)").Scan(&maxArchiveID).Error; err != nil {
		return err
	}
	scope := repository.UsageCostRecalculationScope{Start: start, End: end, MaxEventID: maxEventID, MaxArchiveEventID: maxArchiveID}
	total, err := repository.CountUsageCostRecalculationEvents(ctx, s.db, scope)
	if err != nil {
		return err
	}
	marker, err := loadPricingRecalculationMarker(ctx, s.db)
	if err != nil {
		return err
	}
	if marker == nil {
		return fmt.Errorf("recalculation interruption marker is missing")
	}
	if marker.Scope != nil && marker.Count != nil {
		// IDs survive hot-to-archive movement. A retained target disappearing
		// after a crash means retention made a complete retry impossible.
		previousScope := *marker.Scope
		bound := max(previousScope.MaxEventID, previousScope.MaxArchiveEventID)
		previousScope.MaxEventID, previousScope.MaxArchiveEventID = bound, bound
		retained, err := repository.CountUsageCostRecalculationEvents(ctx, s.db, previousScope)
		if err != nil {
			return err
		}
		if retained != *marker.Count {
			return fmt.Errorf("interrupted recalculation history no longer fully retained")
		}
	}
	marker.Scope, marker.Count = &scope, &total
	if err := savePricingRecalculationMarker(ctx, s.db, *marker); err != nil {
		return fmt.Errorf("save recalculation scope: %w", err)
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
	for _, archive := range []bool{false, true} {
		for cursor := int64(0); ; {
			var events []entities.UsageEvent
			var next int64
			var done bool
			var err error
			if archive {
				events, next, done, err = repository.LoadArchivedUsageCostRecalculationPage(ctx, s.db, scope, cursor)
			} else {
				events, next, done, err = repository.LoadUsageCostRecalculationPage(ctx, s.db, scope, cursor)
			}
			if err != nil {
				return err
			}
			if len(events) > 0 {
				if archive {
					err = repository.ApplyArchivedUsageCostRecalculationBatch(ctx, s.db, scope, events, resolver)
				} else {
					err = repository.ApplyUsageCostRecalculationBatch(ctx, s.db, scope, events, resolver)
				}
				if err != nil {
					return err
				}
				s.addPricingRecalculationProcessed(taskID, int64(len(events)))
			}
			if done {
				break
			}
			cursor = next
		}
	}
	// Every page commits events and both aggregates atomically. Re-summing raw
	// events would discard contributions whose archive retention has expired.
	return nil
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

// setPricingRecalculationStage publishes the current phase without declaring completion early.
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
