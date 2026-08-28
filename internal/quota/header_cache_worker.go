package quota

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	// usageHeaderSnapshotFlushInterval 是 usage response header 快照的默认批量落 cache 间隔。
	usageHeaderSnapshotFlushInterval = time.Minute
	// usageHeaderPendingIdentityLimit 限制一个窗口内不同身份的内存占用；已有身份仍允许更新。
	usageHeaderPendingIdentityLimit = 1000
)

func newUsageHeaderTimer(delay time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(delay)
	return timer.C, func() { timer.Stop() }
}

func (s *Service) TryAppendUsageHeaderSnapshots(snapshots []*UsageHeaderSnapshot) bool {
	// nil service 或空快照没有需要排队的工作，按成功 no-op 处理。
	if s == nil || len(snapshots) == 0 {
		return true
	}
	// 快照在 BuildUsageHeaderSnapshot 发布后完全不可变；这里只复制指针，不再深拷贝 Header/map/slice。
	// usageHeaderMu 同时保护关闭标记和有界 latest map，避免 Stop 与 Append 并发竞态。
	s.usageHeaderMu.Lock()
	if s.usageHeaderClosing {
		s.usageHeaderMu.Unlock()
		return false
	}
	if s.usageHeaderPending == nil {
		s.usageHeaderPending = make(map[string]*UsageHeaderSnapshot, usageHeaderPendingIdentityLimit)
	}
	// 同身份直接覆盖最新值；不同身份总量由同一个 map 的 1000 上限统一约束。
	mergePendingUsageHeaderSnapshotPointers(s.usageHeaderPending, snapshots)
	hasPending := len(s.usageHeaderPending) > 0
	s.usageHeaderMu.Unlock()
	// history fan-out 只复制同一快照指针；队列满只丢历史候选，不影响已经接收的 cache latest-map。
	historyRejected := 0
	for _, snapshot := range snapshots {
		if !s.tryAppendCodexQuotaHistorySnapshot(snapshot) {
			historyRejected++
		}
	}
	if historyRejected > 0 {
		logrus.WithField("rejected_snapshot_count", historyRejected).Warn("codex quota history Header append skipped")
	}
	if !hasPending {
		return true
	}
	// wake 容量 1 只表达“有待处理数据”，连续提交自然合并且永不阻塞 usage 落库后链路。
	select {
	case s.usageHeaderWake <- struct{}{}:
	default:
	}
	return true
}

func (s *Service) runUsageHeaderSnapshotWorker() {
	// worker 退出时通知 StopRefreshTasks，保证关闭流程能等待最后一次 flush 完成。
	defer close(s.usageHeaderDoneCh)
	// 优先使用 service 构造时保存的间隔，方便测试通过 options 覆盖。
	flushInterval := s.usageHeaderFlushInterval
	// 非正间隔没有业务意义，统一回退到 1 分钟默认值。
	if flushInterval <= 0 {
		flushInterval = usageHeaderSnapshotFlushInterval
	}
	// 空闲时 timer channel 与 stop 都为 nil；第一条有效批次才创建一次性窗口。
	var timerC <-chan time.Time
	var stopTimer func()
	// worker 生命周期内持续接收入队快照、定时 flush，或响应关闭信号。
	for {
		select {
		// wake 只表示 latest map 非空；同一窗口内的后续 wake 不重置 timer。
		case <-s.usageHeaderWake:
			if timerC == nil && s.hasPendingUsageHeaderSnapshots() {
				timerC, stopTimer = s.usageHeaderNewTimer(flushInterval)
			}
		// timer 到期原子换出当前 map；apply 期间的新数据进入新的 map 和下一窗口。
		case <-timerC:
			timerC = nil
			stopTimer = nil
			s.flushPendingUsageHeaderSnapshots()
		// 关闭时停止 timer 并 flush 已接受的 latest map。
		case <-s.usageHeaderStopCh:
			if stopTimer != nil {
				stopTimer()
			}
			s.flushPendingUsageHeaderSnapshots()
			return
		}
	}
}

func (s *Service) hasPendingUsageHeaderSnapshots() bool {
	s.usageHeaderMu.Lock()
	defer s.usageHeaderMu.Unlock()
	return len(s.usageHeaderPending) > 0
}

func (s *Service) takePendingUsageHeaderSnapshots() []*UsageHeaderSnapshot {
	// 换出 map 的临界区是 O(1)；排序和数据库处理都在锁外执行，不阻塞提交后 append。
	s.usageHeaderMu.Lock()
	pending := s.usageHeaderPending
	s.usageHeaderPending = make(map[string]*UsageHeaderSnapshot, usageHeaderPendingIdentityLimit)
	s.usageHeaderMu.Unlock()
	return pendingUsageHeaderSnapshots(pending)
}

func (s *Service) flushPendingUsageHeaderSnapshots() {
	// 先原子取得当前窗口的最新身份集合；空集合不查库也不写 cache。
	snapshots := s.takePendingUsageHeaderSnapshots()
	if len(snapshots) == 0 {
		return
	}
	// 真正的身份匹配、窗口统计和 quota cache 合并都集中在 apply 阶段。
	s.applyUsageHeaderSnapshotPointers(context.Background(), snapshots)
}

func mergePendingUsageHeaderSnapshotPointers(pending map[string]*UsageHeaderSnapshot, snapshots []*UsageHeaderSnapshot) {
	rejected := 0
	// 遍历本次入队批次，把同一 flush 窗口内的快照合并到 pending map。
	for _, snapshot := range snapshots {
		// nil 指针没有任何身份或 cache 输出，直接忽略且不占 pending 容量。
		if snapshot == nil {
			continue
		}
		// 优先按 auth_index 合并，保证同一 Codex Auth File 只保留最新进度。
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		// auth_index 缺失的异常快照用 provider/auth_type 分组，避免全部挤到空 key。
		if authIndex == "" {
			authIndex = snapshot.Provider + "\x00" + snapshot.AuthType
		}
		// 达到上限后只拒绝新身份；已经存在的身份继续按时间覆盖，保证最新进度不会被旧值卡住。
		if _, exists := pending[authIndex]; !exists && len(pending) >= usageHeaderPendingIdentityLimit {
			rejected++
			continue
		}
		// 没有旧值或新快照时间更新时覆盖，确保 flush 时使用窗口内最新 header。
		if existing, ok := pending[authIndex]; !ok || usageHeaderSnapshotIsNewer(snapshot, existing) {
			pending[authIndex] = snapshot
		}
	}
	if rejected > 0 {
		logrus.WithFields(logrus.Fields{
			"rejected_snapshot_count": rejected,
			"pending_identity_limit":  usageHeaderPendingIdentityLimit,
		}).Warn("usage header quota pending identity limit reached")
	}
}

// mergePendingUsageHeaderSnapshots 保留值批次测试入口；生产路径始终使用共享指针版本。
func mergePendingUsageHeaderSnapshots(pending map[string]UsageHeaderSnapshot, snapshots []UsageHeaderSnapshot) {
	// 测试兼容层把本地值地址交给生产合并逻辑，不进行 Header 或嵌套对象深拷贝。
	pointerPending := make(map[string]*UsageHeaderSnapshot, len(pending))
	for key, snapshot := range pending {
		cloned := snapshot
		pointerPending[key] = &cloned
	}
	pointers := make([]*UsageHeaderSnapshot, 0, len(snapshots))
	for index := range snapshots {
		pointers = append(pointers, &snapshots[index])
	}
	mergePendingUsageHeaderSnapshotPointers(pointerPending, pointers)
	// 将测试可观察结果写回值 map；生产 Service 不调用该兼容层。
	clear(pending)
	for key, snapshot := range pointerPending {
		if snapshot != nil {
			pending[key] = *snapshot
		}
	}
}

func usageHeaderSnapshotIsNewer(candidate *UsageHeaderSnapshot, existing *UsageHeaderSnapshot) bool {
	// nil candidate 无法覆盖有效快照；nil existing 则允许任意有效 candidate 初始化。
	if candidate == nil {
		return false
	}
	if existing == nil {
		return true
	}
	if candidate.ObservedAt.IsZero() {
		return existing.ObservedAt.IsZero()
	}
	if existing.ObservedAt.IsZero() {
		return true
	}
	return !candidate.ObservedAt.Before(existing.ObservedAt)
}

func pendingUsageHeaderSnapshots(pending map[string]*UsageHeaderSnapshot) []*UsageHeaderSnapshot {
	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	snapshots := make([]*UsageHeaderSnapshot, 0, len(keys))
	for _, key := range keys {
		snapshots = append(snapshots, pending[key])
	}
	return snapshots
}

func (s *Service) stopUsageHeaderSnapshotWorker() {
	if s == nil || s.usageHeaderStopCh == nil || s.usageHeaderDoneCh == nil {
		return
	}
	s.usageHeaderCloseOnce.Do(func() {
		s.usageHeaderMu.Lock()
		s.usageHeaderClosing = true
		close(s.usageHeaderStopCh)
		s.usageHeaderMu.Unlock()
		<-s.usageHeaderDoneCh
	})
}

func (s *Service) applyUsageHeaderSnapshots(ctx context.Context, snapshots []UsageHeaderSnapshot) {
	// 值批次只保留给现有定向测试和同步 helper；生产 worker 使用不可变指针版本。
	pointers := make([]*UsageHeaderSnapshot, 0, len(snapshots))
	for index := range snapshots {
		pointers = append(pointers, &snapshots[index])
	}
	s.applyUsageHeaderSnapshotPointers(ctx, pointers)
}

func (s *Service) applyUsageHeaderSnapshotPointers(ctx context.Context, snapshots []*UsageHeaderSnapshot) {
	// nil service 或没有快照时直接返回，保持批量 apply 和 flush 空批语义一致。
	if s == nil || len(snapshots) == 0 {
		return
	}
	// 批量查询活跃 Codex Auth File 身份，避免每个 snapshot 单独查一次 usage_identities。
	identityByAuthIndex, err := s.usageHeaderIdentityLookup(ctx, snapshots)
	// 身份查询失败时整批跳过，避免在身份状态不确定时写入错误账号的 quota cache。
	if err != nil {
		logrus.WithError(err).WithField("snapshot_count", len(snapshots)).Warn("usage header quota identity lookup failed")
		return
	}
	// header quota 还要补本地窗口 token/cost，因此每批复用一个窗口统计 provider。
	statsProvider := s.usageHeaderWindowStatsProvider(ctx)
	if statsProvider == nil {
		// 批量 header 更新必须与窗口 token/cost 使用同一统计基础；统计器不可用时整批跳过，避免写入半套 cache。
		return
	}
	// 先构造身份 job；缺失或非 Codex 身份在主 goroutine 过滤，worker 只处理明确归属的账号。
	type usageHeaderSnapshotJob struct {
		// snapshot 是 Build 阶段冻结的只读对象，两个临时 worker 都不能修改其内部投影。
		snapshot *UsageHeaderSnapshot
		// identity 是当前 auth_index 对应的活跃 Codex Auth File 数据库事实。
		identity entities.UsageIdentity
	}
	jobs := make([]usageHeaderSnapshotJob, 0, len(snapshots))
	for _, snapshot := range snapshots {
		// nil 指针可能来自关闭竞态或测试异常输入，不能解引用或创建 job。
		if snapshot == nil {
			continue
		}
		// auth_index 在入 cache 前再 trim 一次，避免空白导致 identity map 匹配失败。
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		// 找不到活跃 Codex 身份时跳过当前 snapshot，不影响同批其它账号。
		identity, ok := identityByAuthIndex[authIndex]
		if !ok {
			logUsageHeaderSnapshotIgnored(snapshot)
			continue
		}
		jobs = append(jobs, usageHeaderSnapshotJob{snapshot: snapshot, identity: identity})
	}
	if len(jobs) == 0 {
		return
	}

	// 固定两个临时 worker 只并发不同身份；同一身份已在 pending 阶段合并成一个 job。
	workerCount := min(2, len(jobs))
	jobCh := make(chan usageHeaderSnapshotJob)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for job := range jobCh {
				// 单账号失败只留下 debug，不取消或阻塞同批其它身份。
				if !s.applyUsageHeaderSnapshotWithIdentity(ctx, job.snapshot, job.identity, statsProvider) {
					logUsageHeaderSnapshotIgnored(job.snapshot)
				}
			}
		}()
	}
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	workers.Wait()
}

func logUsageHeaderSnapshotIgnored(snapshot *UsageHeaderSnapshot) {
	// nil 快照没有可诊断身份，保持静默即可。
	if snapshot == nil {
		return
	}
	logrus.WithFields(logrus.Fields{
		"auth_index": snapshot.AuthIndex,
		"provider":   snapshot.Provider,
	}).Debug("usage header quota snapshot ignored")
}

func (s *Service) usageHeaderIdentityLookup(ctx context.Context, snapshots []*UsageHeaderSnapshot) (map[string]entities.UsageIdentity, error) {
	// 先从 snapshot 中抽取可查询的 OAuth auth_index 集合。
	authIndexes := usageHeaderSnapshotAuthIndexes(snapshots)
	// 没有可查询 auth_index 时返回空 map，后续 apply 会逐条 debug 跳过。
	if len(authIndexes) == 0 {
		return map[string]entities.UsageIdentity{}, nil
	}
	// 只查询活跃 Auth File 身份，删除/禁用账号不接受 header cache 更新。
	identities, err := repository.ListActiveAuthFileUsageIdentitiesByAuthIndexes(ctx, s.db, authIndexes)
	if err != nil {
		return nil, err
	}
	// 结果 map 按 auth_index 建索引，供批量 apply O(1) 匹配。
	identityByAuthIndex := make(map[string]entities.UsageIdentity, len(identities))
	// 遍历仓储返回结果，只保留 type=codex 的 Auth File。
	for _, identity := range identities {
		// identity 字段就是 auth_index，入 map 前统一 trim。
		authIndex := strings.TrimSpace(identity.Identity)
		// 空 identity 或非 Codex 类型不能被 header snapshot 更新。
		if authIndex == "" || !usageHeaderIdentityIsCodex(identity) {
			continue
		}
		// 同一 auth_index 只需要一条活跃身份记录。
		identityByAuthIndex[authIndex] = identity
	}
	// 返回可用于匹配的 Codex Auth File 身份集合。
	return identityByAuthIndex, nil
}

func usageHeaderSnapshotAuthIndexes(snapshots []*UsageHeaderSnapshot) []string {
	// authIndexes 保存去重后的 OAuth auth_index 查询参数。
	authIndexes := make([]string, 0, len(snapshots))
	// seen 用来避免同一 auth_index 在批量查询中重复出现。
	seen := make(map[string]struct{}, len(snapshots))
	// 遍历 snapshot，只提取 Auth File OAuth 来源的账号标识。
	for _, snapshot := range snapshots {
		// nil 快照不具备任何身份字段，直接跳过。
		if snapshot == nil {
			continue
		}
		// auth_type 大小写不稳定时统一转小写比较。
		authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
		// auth_index 也统一 trim，保证查询参数干净。
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		// 非 OAuth 或空 auth_index 的 snapshot 不能匹配 Auth File，后续会被跳过。
		if authType != "oauth" || authIndex == "" {
			continue
		}
		// 已加入过的 auth_index 不重复追加。
		if _, ok := seen[authIndex]; ok {
			continue
		}
		// 记录当前 auth_index 已出现。
		seen[authIndex] = struct{}{}
		// 追加到最终查询列表。
		authIndexes = append(authIndexes, authIndex)
	}
	// 返回稳定保留首次出现顺序的 auth_index 列表。
	return authIndexes
}

func (s *Service) usageHeaderWindowStatsProvider(ctx context.Context) usageWindowStatsProvider {
	calculator, err := repository.NewUsageWindowStatsCalculator(ctx, s.db, s.pricing.NewResolver())
	if err != nil {
		logrus.WithError(err).Debug("usage header quota window stats calculator unavailable")
		return nil
	}
	return calculator
}

func (s *Service) applyUsageHeaderSnapshot(ctx context.Context, snapshot UsageHeaderSnapshot) bool {
	// nil service 不能查身份或写 cache，直接视为未应用。
	if s == nil {
		return false
	}
	// 单条 apply 入口同样只接受 OAuth Auth File 快照。
	authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
	// auth_index 是后续查身份和写 refreshTasks 的唯一键。
	authIndex := strings.TrimSpace(snapshot.AuthIndex)
	// 非 OAuth 或缺少 auth_index 的 snapshot 不具备更新 Codex quota cache 的身份边界。
	if authType != "oauth" || authIndex == "" {
		return false
	}
	// 单条路径按 auth_index 查询当前活跃 Auth File 身份。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	// 身份不存在或查询异常时跳过；非 not-found 错误由 helper 记录 warning。
	if err != nil {
		logUsageHeaderIdentityLookupError(authIndex, err)
		return false
	}
	// 只有 type=codex 的 Auth File 才允许消费 X-Codex-* header。
	if !usageHeaderIdentityIsCodex(identity) {
		return false
	}
	// 身份确认后进入共享 apply 逻辑，单条路径按需自行构造窗口统计。
	return s.applyUsageHeaderSnapshotWithIdentity(ctx, &snapshot, identity, nil)
}

func logUsageHeaderIdentityLookupError(authIndex string, err error) {
	// not found 是常见跳过路径，不需要把普通缺失账号提升到 warning。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	// 真实数据库错误需要 warning，方便排查 header cache 长期无法更新的问题。
	logrus.WithError(err).WithField("auth_index", authIndex).Warn("usage header quota identity lookup failed")
}

func (s *Service) applyUsageHeaderSnapshotWithIdentity(ctx context.Context, snapshot *UsageHeaderSnapshot, identity entities.UsageIdentity, statsProvider usageWindowStatsProvider) bool {
	// nil service 不能继续解析、统计或写入 cache。
	if s == nil || snapshot == nil {
		return false
	}
	// auth_type 在最终 apply 前再标准化一次，防止测试或单条入口绕过前置校验。
	authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
	// auth_index 标准化后必须和传入 identity 对齐。
	authIndex := strings.TrimSpace(snapshot.AuthIndex)
	// 快照身份和 usage identity 不一致时直接跳过，避免串写其它账号 cache。
	if authType != "oauth" || authIndex == "" || strings.TrimSpace(identity.Identity) != authIndex {
		return false
	}
	// 再次确认 identity 类型是 Codex，保证批量路径和单条路径共享同一安全边界。
	if !usageHeaderIdentityIsCodex(identity) {
		return false
	}
	// 直接读取 Build 阶段生成的只读 cache 投影，分钟 worker 不再解析或持有 Header。
	output := snapshot.CacheOutput
	// 将 provider 输出标准化成前端缓存使用的 CheckResponse。
	response := CheckResponse{
		ID:           authIndex,
		Quota:        NormalizeQuotaRows(output),
		Subscription: NormalizeSubscription(output),
	}
	// 没有可展示 quota row 时不写空 cache，避免覆盖已有有效结果。
	if len(response.Quota) == 0 {
		return false
	}
	// reset credit 只有 Codex 官方查询/header 支持，存在时写入同一份 cache。
	if count, ok := rateLimitResetCreditsAvailableCount(output); ok {
		response.RateLimitResetCreditsAvailableCount = count
	}
	// observedAt 使用项目统一存储时间口径，后续用它做新旧 cache 比较和窗口统计截止点。
	observedAt := timeutil.NormalizeStorageTime(snapshot.ObservedAt)
	// 上游时间缺失时用当前时间兜底，避免创建零时间的已完成 cache。
	if observedAt.IsZero() {
		observedAt = timeutil.NormalizeStorageTime(time.Now())
	}
	// active task 或更新的 completed cache 已存在时，当前 header snapshot 不应覆盖它。
	if !s.shouldProcessUsageHeaderQuotaSnapshot(authIndex, observedAt) {
		return false
	}
	// 批量路径传入 statsProvider，用同一统计器复用 price/settings 查询。
	if statsProvider != nil {
		response = s.attachWindowUsageStatsWithProvider(ctx, authIndex, response, observedAt, statsProvider)
	} else {
		// 单条路径没有共享 provider 时走现有 helper 自行构造窗口统计。
		response = s.attachWindowUsageStats(ctx, authIndex, response, observedAt)
	}
	// 最后把 header quota 与已有 cache 合并，并写回 refreshTasks。
	return s.mergeUsageHeaderQuotaCache(authIndex, response, observedAt, identity)
}

func usageHeaderIdentityIsCodex(identity entities.UsageIdentity) bool {
	// identity.Type 是 Auth File 的真实 provider 类型，使用统一 normalization 判断 codex。
	return normalizeIdentityType(identity.Type) == "codex"
}

func (s *Service) shouldProcessUsageHeaderQuotaSnapshot(authIndex string, observedAt time.Time) bool {
	// refreshTasks 与手动/自动刷新任务共享，读写前必须持有 refreshMu。
	s.refreshMu.Lock()
	// 函数退出时释放任务锁，避免阻塞前端查询或刷新调度。
	defer s.refreshMu.Unlock()
	// 查找当前 auth_index 已有的 quota refresh/cache 记录。
	existing, ok := s.refreshTasks[authIndex]
	// 没有任何 cache 或任务时，header snapshot 可以创建第一份 cache。
	if !ok {
		return true
	}
	// 手动/自动刷新正在 queued/running 时，以主动刷新为准，header snapshot 不抢写。
	if existing.isActive() {
		return false
	}
	// 已有 completed cache 时间不早于当前 header 时，跳过旧 snapshot。
	if usageHeaderCompletedCacheIsCurrentOrNewer(existing, observedAt) {
		return false
	}
	// 已有 cache 更旧或失败时，允许当前 header snapshot 修复/更新 cache。
	return true
}

func (s *Service) mergeUsageHeaderQuotaCache(authIndex string, response CheckResponse, observedAt time.Time, identity entities.UsageIdentity) bool {
	// 写 refreshTasks 前加锁，和主动刷新任务状态切换保持同一同步边界。
	s.refreshMu.Lock()
	// 函数退出时释放锁。
	defer s.refreshMu.Unlock()
	// 读取当前 auth_index 已有任务或 cache，用于防覆盖和合并。
	existing, ok := s.refreshTasks[authIndex]
	// 有旧记录时先确认当前 header 仍然有资格写入。
	if ok {
		// active 任务可能在前置检查后出现，二次检查避免竞态覆盖。
		if existing.isActive() {
			return false
		}
		// completed cache 可能在前置检查后更新，二次检查避免旧 header 回写。
		if usageHeaderCompletedCacheIsCurrentOrNewer(existing, observedAt) {
			return false
		}
		// 有旧 quota 时保留 header 没覆盖的字段，例如 reset credits 或非 header 行。
		if existing.Quota != nil {
			response = mergeUsageHeaderQuotaResponse(*existing.Quota, response)
		}
	}
	// 写入一条 completed refresh task，让前端 quota cache API 可以直接读取。
	s.refreshTasks[authIndex] = &RefreshTaskRecord{
		AuthIndex:   authIndex,
		Name:        helper.UsageIdentityDisplayName(identity),
		Type:        identity.Type,
		FileName:    identity.FileName,
		Status:      RefreshTaskStatusCompleted,
		Quota:       &response,
		Source:      RefreshSourceUsageHeader,
		CreatedAt:   observedAt,
		RefreshedAt: observedAt,
	}
	// 返回 true 表示本次 header snapshot 已经成功写入 cache。
	return true
}

func usageHeaderCompletedCacheIsCurrentOrNewer(existing *RefreshTaskRecord, observedAt time.Time) bool {
	// 只有 completed 且 refreshed_at 有效的 cache 才能参与时间新旧判断。
	return existing != nil &&
		existing.Status == RefreshTaskStatusCompleted &&
		!existing.RefreshedAt.IsZero() &&
		!existing.RefreshedAt.Before(observedAt)
}

func mergeUsageHeaderQuotaResponse(existing CheckResponse, header CheckResponse) CheckResponse {
	// header 是最新来源，先以 header response 作为合并基底。
	merged := header
	// header 缺失 ID 时沿用旧 cache ID，保持前端响应稳定。
	if merged.ID == "" {
		merged.ID = existing.ID
	}
	// header 没带 reset credits 时保留旧 cache 的 reset credit 信息。
	if merged.RateLimitResetCreditsAvailableCount == nil {
		merged.RateLimitResetCreditsAvailableCount = existing.RateLimitResetCreditsAvailableCount
	}
	// Header 未携带套餐只代表这次快照没有该字段，保留此前完整刷新或 Header 已确认的订阅。
	if merged.Subscription == nil {
		merged.Subscription = existing.Subscription
	}
	// quota rows 按 key 合并，header row 覆盖进度，旧 cache 保留非 header 字段。
	merged.Quota = mergeUsageHeaderQuotaRows(existing.Quota, header.Quota)
	// 返回最终合并后的缓存响应。
	return merged
}

func mergeUsageHeaderQuotaRows(existing []QuotaRow, header []QuotaRow) []QuotaRow {
	// headerByKey 用来按 quota key 快速找到 header 中的新进度行。
	headerByKey := make(map[string]QuotaRow, len(header))
	// headerOrder 保留 header 新行的顺序，方便最后追加旧 cache 没有的行。
	headerOrder := make([]string, 0, len(header))
	// 先遍历 header rows 建索引。
	for _, row := range header {
		// 空 key 无法稳定合并，直接丢弃。
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		// 第一次看到该 key 时记录顺序。
		if _, ok := headerByKey[row.Key]; !ok {
			headerOrder = append(headerOrder, row.Key)
		}
		// 同 key 多次出现时以后出现的 header row 为准。
		headerByKey[row.Key] = row
	}
	// merged 预分配旧行和 header 行容量，减少 append 扩容。
	merged := make([]QuotaRow, 0, len(existing)+len(header))
	// seen 记录已经输出过的 key，避免最后追加重复 header 行。
	seen := make(map[string]struct{}, len(existing)+len(header))
	// 先按旧 cache 顺序输出，保证前端 row 顺序稳定。
	for _, row := range existing {
		// 旧 row 有同 key header replacement 时，做字段级合并后输出。
		if replacement, ok := headerByKey[row.Key]; ok {
			merged = append(merged, mergeUsageHeaderQuotaRow(row, replacement))
			seen[row.Key] = struct{}{}
			continue
		}
		// header 没覆盖的旧 row 原样保留。
		merged = append(merged, row)
		// 只有非空 key 才加入 seen，避免空 key 阻挡 header 新行。
		if strings.TrimSpace(row.Key) != "" {
			seen[row.Key] = struct{}{}
		}
	}
	// 旧 cache 不存在的新 header row 按 header 顺序追加。
	for _, key := range headerOrder {
		// 已经在旧 row 阶段输出过的 key 不重复追加。
		if _, ok := seen[key]; ok {
			continue
		}
		// 追加 header 新增的 quota row。
		merged = append(merged, headerByKey[key])
	}
	// 返回稳定顺序的合并结果。
	return merged
}

func mergeUsageHeaderQuotaRow(existing QuotaRow, header QuotaRow) QuotaRow {
	// 以旧 row 为基底，保留 header 没有携带的人工/官方完整字段。
	merged := existing
	// header key 非空时覆盖 key，正常情况下与 existing key 相同。
	if strings.TrimSpace(header.Key) != "" {
		merged.Key = header.Key
	}
	// header label 非空时使用 header 的窗口标签。
	if strings.TrimSpace(header.Label) != "" {
		merged.Label = header.Label
	}
	// header scope 非空时更新行类型。
	if strings.TrimSpace(header.Scope) != "" {
		merged.Scope = header.Scope
	}
	// header metric 非空时更新指标类型。
	if strings.TrimSpace(header.Metric) != "" {
		merged.Metric = header.Metric
	}
	// header 带 absolute used 时覆盖旧 used。
	if header.Used != nil {
		merged.Used = header.Used
	}
	// header 带 limit 时覆盖旧 limit。
	if header.Limit != nil {
		merged.Limit = header.Limit
	}
	// header 带 remaining 时覆盖旧 remaining。
	if header.Remaining != nil {
		merged.Remaining = header.Remaining
	}
	// header 带 remaining fraction 时覆盖旧 fraction。
	if header.RemainingFraction != nil {
		merged.RemainingFraction = header.RemainingFraction
	}
	// header used percent 是这条功能的核心进度字段，存在时覆盖旧进度。
	if header.UsedPercent != nil {
		merged.UsedPercent = header.UsedPercent
	}
	// header 带 allowed 时覆盖旧 allowed。
	if header.Allowed != nil {
		merged.Allowed = header.Allowed
	}
	// header 带 limit reached 时覆盖旧 limit reached。
	if header.LimitReached != nil {
		merged.LimitReached = header.LimitReached
	}
	// header 带窗口信息时覆盖旧窗口定义。
	if header.Window != nil {
		merged.Window = header.Window
	}
	// header 带 resetAt 时覆盖旧重置时间。
	if strings.TrimSpace(header.ResetAt) != "" {
		merged.ResetAt = header.ResetAt
	}
	// header 带 resetAfterSeconds 时覆盖旧相对重置时间。
	if header.ResetAfterSeconds != nil {
		merged.ResetAfterSeconds = header.ResetAfterSeconds
	}
	// 普通窗口 row 的 token/cost 要跟最新窗口重新计算，哪怕为空也要清掉旧值。
	if usageHeaderQuotaRowIsWindow(header) {
		// Header 只负责刷新普通 window 进度条；token/cost 跟随同一窗口重新计算，即使为空也要清掉旧值。
		merged.WindowUsageTokens = header.WindowUsageTokens
		merged.WindowUsageCost = header.WindowUsageCost
	} else {
		// 非普通窗口 row 只有 header 明确带 token 时才覆盖旧 token。
		if header.WindowUsageTokens != nil {
			merged.WindowUsageTokens = header.WindowUsageTokens
		}
		// 非普通窗口 row 只有 header 明确带 cost 时才覆盖旧 cost。
		if header.WindowUsageCost != nil {
			merged.WindowUsageCost = header.WindowUsageCost
		}
	}
	// 返回合并后的单行 quota。
	return merged
}

func usageHeaderQuotaRowIsWindow(row QuotaRow) bool {
	// scope=window 表示普通 quota 窗口，需要同步刷新 token/cost fallback。
	return strings.EqualFold(strings.TrimSpace(row.Scope), "window")
}
