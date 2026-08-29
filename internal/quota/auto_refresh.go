package quota

import (
	"context"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
)

const autoRefreshSettingsCheckInterval = time.Minute

type autoRefreshWakeReason int

const (
	autoRefreshWakeElapsed autoRefreshWakeReason = iota
	autoRefreshWakeSettingsChanged
	autoRefreshWakeCanceled
)

type authFileRefreshRoundOptions struct {
	source              RefreshSource
	skipCachedHTTPError bool
}

type authFileRefreshRoundSummary struct {
	scanned            int
	queued             int
	skippedCachedError int
	skippedRunning     int
	skippedUnsupported int
	queuedAuthIndexes  []string
	roundAuthIndexes   []string
}

func (s *Service) RunAutoRefresh(ctx context.Context) error {
	// nil service 或未初始化数据库时没有可刷新对象，直接安全返回。
	if s == nil || s.db == nil {
		return nil
	}
	// now 作为本轮调度时间，后续轮次互斥和缓存过期判断都复用它。
	now := time.Now()
	// 同一时间只允许一个自动刷新轮次存活；调度频率由 StartAutoRefresh 读取持久配置控制。
	if !s.beginAutoRefreshRound(now) {
		// 上一轮仍 active 时直接跳过，手动刷新仍可走共享任务队列。
		logrus.Debug("quota auto refresh skipped because previous auto round is still active")
		return nil
	}
	// 该标记用于区分“本 goroutine 自己收尾”和“任务监控 goroutine 收尾”。
	roundHandedToMonitor := false
	logrus.Info("quota auto refresh round started")
	// 每轮结束日志用 defer 兜底，确保扫描失败、无任务入队或同步完成时都能释放轮次锁。
	defer func() {
		// 如果本轮没有交给后台监控，就在当前调用栈释放轮次锁。
		if !roundHandedToMonitor {
			// 扫描失败或没有任务入队时没有监控 goroutine，必须立即释放。
			s.finishAutoRefreshRound()
			logrus.Info("quota auto refresh round completed")
		}
	}()
	// 自动刷新每轮开始先清理过期任务，确保 401/402 过期后能重新进入队列，而不是被旧缓存一直拦住。
	s.cleanupExpiredRefreshTasks(now)
	summary, err := s.queueAuthFileRefreshRound(ctx, now, authFileRefreshRoundOptions{
		source:              RefreshSourceScheduled,
		skipCachedHTTPError: true,
	})
	if err != nil {
		return err
	}
	// Auth Files 扫描成功后才记录整轮启动时间；扫描失败只依赖 attempt 时间做轻量退避。
	s.markAutoRefreshRoundStartedAt(now)
	if len(summary.queuedAuthIndexes) > 0 {
		if s.startRefreshGoroutine(func() {
			s.dispatchAutoRefreshTasks(summary.queuedAuthIndexes)
		}) {
			roundHandedToMonitor = true
		} else {
			// 关闭期间不能再启动 dispatcher，本轮已入队任务必须转为失败，避免永久停在 queued。
			s.markQueuedRefreshTasksFailed(summary.queuedAuthIndexes, context.Canceled)
		}
	}
	logrus.WithFields(logrus.Fields{
		"scanned":              summary.scanned,
		"queued":               summary.queued,
		"skipped_cached_error": summary.skippedCachedError,
		"skipped_running":      summary.skippedRunning,
		"skipped_unsupported":  summary.skippedUnsupported,
	}).Debug("quota auto refresh round summary")
	return nil
}

func (s *Service) queueAuthFileRefreshRound(ctx context.Context, now time.Time, options authFileRefreshRoundOptions) (authFileRefreshRoundSummary, error) {
	identities, err := s.listAutoRefreshAuthFiles(ctx)
	if err != nil {
		return authFileRefreshRoundSummary{}, err
	}
	summary := authFileRefreshRoundSummary{
		scanned:           len(identities),
		queuedAuthIndexes: make([]string, 0, len(identities)),
		roundAuthIndexes:  make([]string, 0, len(identities)),
	}
	for _, identity := range identities {
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" {
			continue
		}
		if _, _, ok := s.resolveQuotaHandlerForIdentity(identity); !ok {
			// 自动刷新和手动巡检共用同一套 handler 解析逻辑，不支持的 Auth File 不占用 worker。
			summary.skippedUnsupported++
			continue
		}
		if options.skipCachedHTTPError && s.shouldSkipAutoRefreshForCachedHTTPError(authIndex, now) {
			// 自动刷新跳过未过期的可缓存 HTTP 错误；手动巡检会先清缓存并重新尝试。
			summary.skippedCachedError++
			continue
		}
		if task, created := s.ensureRefreshTaskWithIdentity(authIndex, options.source, identity); created {
			summary.queued++
			summary.queuedAuthIndexes = append(summary.queuedAuthIndexes, task.AuthIndex)
			summary.roundAuthIndexes = append(summary.roundAuthIndexes, task.AuthIndex)
		} else if task != nil && task.isActive() {
			// queued/running 已经代表这个 auth_index 在队列里，同一轮不能重复入队。
			summary.skippedRunning++
			if task.Source == options.source {
				// 只有同来源的 active task 才能被当前轮次收养；巡检不能把手动/自动刷新算成巡检 running。
				summary.roundAuthIndexes = append(summary.roundAuthIndexes, task.AuthIndex)
			}
		}
	}
	return summary, nil
}

func (s *Service) beginAutoRefreshRound(now time.Time) bool {
	// now 归一化后写入内存时间，方便后续和配置间隔比较。
	now = timeutil.NormalizeStorageTime(now)
	// 自动刷新轮次锁独立于任务锁，避免长时间扫描时阻塞前端轮询任务状态。
	s.autoRefreshMu.Lock()
	// defer 解锁，保证所有返回路径都释放轮次锁。
	defer s.autoRefreshMu.Unlock()
	// 已有轮次存活时拒绝新轮次，减少重复 DB 扫描和重复排队判断。
	if s.autoRefreshRunning {
		return false
	}
	// 标记当前自动刷新轮次开始，后续由当前调用栈或监控 goroutine 释放。
	s.autoRefreshRunning = true
	// 记录最近一次尝试时间；即使扫描失败，也要用它为下一次尝试提供退避。
	s.lastAutoRefreshAttemptAt = now
	return true
}

func (s *Service) markAutoRefreshRoundStartedAt(now time.Time) {
	// now 归一化后写入内存时间，和 beginAutoRefreshRound 的 interval 判断保持一致。
	now = timeutil.NormalizeStorageTime(now)
	// autoRefreshMu 保护 lastAutoRefreshRoundAt，避免定时触发和任务监控并发读写。
	s.autoRefreshMu.Lock()
	// defer 解锁，保证写入完成后释放轮次锁。
	defer s.autoRefreshMu.Unlock()
	// 记录本轮整表自动刷新启动时间，后续调度循环用它控制下一轮间隔。
	s.lastAutoRefreshRoundAt = now
}

func (s *Service) resetAutoRefreshScheduleAnchor() {
	if s == nil {
		return
	}
	// 配置更新后下一次调度按新配置的首次触发语义计算，不能沿用旧轮次或失败退避锚点。
	s.autoRefreshMu.Lock()
	defer s.autoRefreshMu.Unlock()
	s.lastAutoRefreshRoundAt = time.Time{}
	s.lastAutoRefreshAttemptAt = time.Time{}
}

func (s *Service) finishAutoRefreshRound() {
	// 释放轮次状态前加锁，和 beginAutoRefreshRound 的读写保持一致。
	s.autoRefreshMu.Lock()
	// defer 解锁，保证状态写入后正常释放锁。
	defer s.autoRefreshMu.Unlock()
	// 标记本轮自动刷新结束，下一次 tick 才可以重新扫描 Auth Files。
	s.autoRefreshRunning = false
}

func (s *Service) dispatchAutoRefreshTasks(authIndexes []string) {
	// 自动刷新轮次从扫描到最后一个本轮任务完成都算 active，防止下一次 tick 又为已完成的前半批重复入队。
	// defer 确保 dispatcher 退出、等待结束或 refreshContext 取消时都会释放轮次锁。
	defer func() {
		s.finishAutoRefreshRound()
		logrus.Info("quota auto refresh round completed")
	}()
	// 复用共享 dispatcher，继续使用全局 worker limit 和关闭时 queued 任务失败逻辑。
	s.dispatchRefreshTasks(authIndexes)
	// dispatcher 只负责派发，派发后还要等本轮 auto 任务全部离开 queued/running。
	s.waitForAutoRefreshTasks(authIndexes)
}

func (s *Service) waitForAutoRefreshTasks(authIndexes []string) {
	// 1s 轮询足够轻量，且只在自动刷新轮次存在期间运行一个监控 goroutine。
	ticker := time.NewTicker(time.Second)
	// 函数退出时释放 ticker，避免长期泄漏 runtime timer。
	defer ticker.Stop()
	refreshDone := s.refreshContextSnapshot().Done()
	for {
		// 没有本轮 active 任务时，说明 queued/running 已全部完成或失败，可以结束轮次。
		if !s.hasActiveRefreshTask(authIndexes) {
			return
		}
		select {
		// 应用关闭时不再等待，任务本身会通过 refreshContext 进入取消/失败路径。
		case <-refreshDone:
			return
		// 到下一个轻量检查周期后再次确认本轮任务状态。
		case <-ticker.C:
		}
	}
}

func (s *Service) hasActiveRefreshTask(authIndexes []string) bool {
	// 读取 refreshTasks 前加锁，和任务状态切换、清理逻辑保持同一把锁。
	s.refreshMu.Lock()
	// defer 解锁，保证任何返回路径都释放任务锁。
	defer s.refreshMu.Unlock()
	for _, authIndex := range authIndexes {
		// 按本轮入队的 auth_index 查任务，避免扫描整个任务 map。
		task, ok := s.refreshTasks[authIndex]
		// 只把本轮定时刷新任务的 queued/running 算作轮次仍 active。
		if ok && task.Source == RefreshSourceScheduled && task.isActive() {
			return true
		}
	}
	// 所有本轮任务都不再 active，监控 goroutine 可以释放自动刷新轮次。
	return false
}

func (s *Service) StartAutoRefresh(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		settings, err := s.GetAutoRefreshSettings(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logrus.Errorf("quota auto refresh settings lookup failed: %v", err)
			if s.sleepAutoRefreshDelay(ctx, autoRefreshSettingsCheckInterval) == autoRefreshWakeCanceled {
				return nil
			}
			continue
		}
		if !settings.Enabled || settings.Schedule == nil {
			if s.sleepAutoRefreshDelay(ctx, autoRefreshSettingsCheckInterval) == autoRefreshWakeCanceled {
				return nil
			}
			continue
		}

		delay := s.nextAutoRefreshDelay(settings, s.currentAutoRefreshTime())
		if delay > 0 {
			switch s.sleepAutoRefreshDelay(ctx, delay) {
			case autoRefreshWakeCanceled:
				return nil
			case autoRefreshWakeSettingsChanged:
				continue
			}
		}
		if err := s.RunAutoRefresh(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logrus.Errorf("quota auto refresh failed: %v", err)
		}
	}
}

func (s *Service) currentAutoRefreshTime() time.Time {
	if s != nil && s.autoRefreshNow != nil {
		return s.autoRefreshNow()
	}
	return time.Now()
}

func (s *Service) sleepAutoRefreshDelay(ctx context.Context, delay time.Duration) autoRefreshWakeReason {
	if s == nil {
		return autoRefreshWakeCanceled
	}
	if delay <= 0 {
		return autoRefreshWakeElapsed
	}
	if s != nil && s.autoRefreshDelay != nil {
		if !s.autoRefreshDelay(ctx, delay) {
			return autoRefreshWakeCanceled
		}
		return autoRefreshWakeElapsed
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return autoRefreshWakeCanceled
	case <-s.autoRefreshSettingsChanged:
		return autoRefreshWakeSettingsChanged
	case <-timer.C:
		return autoRefreshWakeElapsed
	}
}

func (s *Service) notifyAutoRefreshSettingsChanged() {
	if s == nil || s.autoRefreshSettingsChanged == nil {
		return
	}
	select {
	case s.autoRefreshSettingsChanged <- struct{}{}:
	default:
	}
}

func (s *Service) nextAutoRefreshDelay(settings AutoRefreshSettings, now time.Time) time.Duration {
	if s == nil {
		return autoRefreshSettingsCheckInterval
	}
	if !settings.Enabled || settings.Schedule == nil {
		return autoRefreshSettingsCheckInterval
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 自动刷新轮次状态由 autoRefreshMu 保护，读取 running 和 lastAutoRefreshRoundAt 前必须加锁。
	s.autoRefreshMu.Lock()
	running := s.autoRefreshRunning
	anchor := s.lastAutoRefreshRoundAt
	lastAttempt := s.lastAutoRefreshAttemptAt
	s.autoRefreshMu.Unlock()

	if running {
		if anchor.IsZero() {
			return autoRefreshSettingsCheckInterval
		}
		dueAt := nextAutoRefreshDueAt(anchor, settings.Schedule)
		if !normalizedNow.Before(dueAt) {
			return autoRefreshSettingsCheckInterval
		}
		return dueAt.Sub(normalizedNow)
	}
	if !lastAttempt.IsZero() && (anchor.IsZero() || lastAttempt.After(anchor)) {
		return autoRefreshSettingsCheckInterval
	}
	if anchor.IsZero() {
		return nextAutoRefreshDelayWithoutAnchor(settings.Schedule, normalizedNow)
	}
	dueAt := nextAutoRefreshDueAt(anchor, settings.Schedule)
	if !normalizedNow.Before(dueAt) {
		return 0
	}
	return dueAt.Sub(normalizedNow)
}

func nextAutoRefreshDelayWithoutAnchor(schedule *AutoRefreshSchedule, now time.Time) time.Duration {
	if schedule == nil {
		return autoRefreshSettingsCheckInterval
	}
	var dueAt time.Time
	switch schedule.Unit {
	case AutoRefreshScheduleUnitMinute:
		dueAt = now.Add(time.Duration(schedule.Value) * time.Minute)
	case AutoRefreshScheduleUnitHour:
		dueAt = now.Add(time.Duration(schedule.Value) * time.Hour)
	case AutoRefreshScheduleUnitDay:
		dueAt = nextLocalMidnightAtOrAfter(now)
	case AutoRefreshScheduleUnitWeek:
		dueAt = nextWeekdayMidnightAtOrAfter(now, schedule.Value)
	default:
		return autoRefreshSettingsCheckInterval
	}
	return delayUntil(now, dueAt)
}

func nextAutoRefreshDueAt(anchor time.Time, schedule *AutoRefreshSchedule) time.Time {
	anchor = timeutil.NormalizeStorageTime(anchor)
	switch schedule.Unit {
	case AutoRefreshScheduleUnitMinute:
		return anchor.Add(time.Duration(schedule.Value) * time.Minute)
	case AutoRefreshScheduleUnitHour:
		return anchor.Add(time.Duration(schedule.Value) * time.Hour)
	case AutoRefreshScheduleUnitDay:
		return nextLocalMidnightAfter(anchor).AddDate(0, 0, schedule.Value-1)
	case AutoRefreshScheduleUnitWeek:
		return nextWeekdayMidnightAfter(anchor, schedule.Value)
	default:
		return anchor.Add(autoRefreshSettingsCheckInterval)
	}
}

func nextLocalMidnightAtOrAfter(now time.Time) time.Time {
	localNow := now.In(time.Local)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	if candidate.Before(localNow) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

func nextLocalMidnightAfter(anchor time.Time) time.Time {
	localAnchor := anchor.In(time.Local)
	candidate := time.Date(localAnchor.Year(), localAnchor.Month(), localAnchor.Day(), 0, 0, 0, 0, time.Local)
	if !candidate.After(localAnchor) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

func nextWeekdayMidnightAtOrAfter(now time.Time, weekdayValue int) time.Time {
	localNow := now.In(time.Local)
	candidate := weekdayMidnight(localNow, weekdayValue)
	if candidate.Before(localNow) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func nextWeekdayMidnightAfter(anchor time.Time, weekdayValue int) time.Time {
	localAnchor := anchor.In(time.Local)
	candidate := weekdayMidnight(localAnchor, weekdayValue)
	if !candidate.After(localAnchor) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func weekdayMidnight(base time.Time, weekdayValue int) time.Time {
	target := time.Weekday(weekdayValue % 7)
	startOfDay := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.Local)
	daysUntilTarget := (int(target) - int(startOfDay.Weekday()) + 7) % 7
	return startOfDay.AddDate(0, 0, daysUntilTarget)
}

func delayUntil(now time.Time, dueAt time.Time) time.Duration {
	if !now.Before(dueAt) {
		return 0
	}
	return dueAt.Sub(now)
}

func (s *Service) listAutoRefreshAuthFiles(ctx context.Context) ([]entities.UsageIdentity, error) {
	var identities []entities.UsageIdentity
	// 自动刷新只扫描未删除且未禁用的 Auth Files；AI Provider 和用户停用的 Auth File 都不应产生后台请求。
	err := s.db.WithContext(ctx).
		Select("id, name, alias, identity, provider, type, file_name, auth_type, is_deleted, disabled").
		Where("auth_type = ? AND is_deleted = ? AND (disabled IS NULL OR disabled = ?)", entities.UsageIdentityAuthTypeAuthFile, false, false).
		Order("priority IS NULL ASC").
		Order("priority DESC").
		Order("id ASC").
		Find(&identities).Error
	return identities, err
}

func (s *Service) shouldSkipAutoRefreshForCachedHTTPError(authIndex string, now time.Time) bool {
	now = timeutil.NormalizeStorageTime(now)
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[authIndex]
	if !ok || task.Status != RefreshTaskStatusFailed || task.HTTPStatusCode == nil {
		return false
	}
	if _, ok := RefreshCacheableHTTPStatusCodes[*task.HTTPStatusCode]; !ok {
		return false
	}
	// 只有未过期的 401/402 等配置错误会拦截自动刷新；过期后下一轮可以重新尝试并覆盖旧错误。
	return task.ExpiresAt.IsZero() || now.Before(task.ExpiresAt)
}
