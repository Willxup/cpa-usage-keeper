package quota

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	// codexQuotaHistoryFlushInterval 是首条数据入队后、固定本批队列边界前的默认等待时长。
	codexQuotaHistoryFlushInterval = 10 * time.Second
	// codexQuotaHistoryQueueSize 限制 usage 热路径最多持有的不可变快照指针数量。
	codexQuotaHistoryQueueSize = 1024
	// codexQuotaHistoryDatabaseTimeout 同时限制身份确认、状态恢复、正常 flush 与 shutdown flush。
	codexQuotaHistoryDatabaseTimeout = 2 * time.Second
	// codexQuotaHistoryResetTolerance 统一容忍上游直接时刻和倒计时推算时刻的两分钟漂移。
	codexQuotaHistoryResetTolerance = 120 * time.Second
)

// codexQuotaHistoryInput 是独立 runner 的有界队列元素；Header 与可信主动查询分别进入自己的 channel。
type codexQuotaHistoryInput struct {
	// Snapshot 是 usage Header 构造阶段唯一分配的只读快照指针；runner 取出 observation 后立即释放。
	Snapshot *UsageHeaderSnapshot
	// Observations 是主动查询成功后构造的最多两条主额度事实；该切片所有权随入队转交 runner。
	Observations []repositorydto.CodexMainQuotaObservation
	// IdentityVerified 表示主动查询入口已经确认活跃 Codex Auth File；Header 来源固定为 false 并批量回查。
	IdentityVerified bool
	// Source 决定 Header/可信队列分流、同账号角色 Header 替代、校准权限与失败日志，不写入数据库。
	Source RefreshSource
}

// codexQuotaHistoryCandidate 是 runner 从完整快照提取后的最小处理单元，不再持有 Header/cache 投影。
type codexQuotaHistoryCandidate struct {
	// Observation 保存单个 Primary/Secondary 状态，不包含 token、cost 或 Header。
	Observation repositorydto.CodexMainQuotaObservation
	// IdentityVerified 区分主动查询已验证来源和仍需批量验证的 Header 来源。
	IdentityVerified bool
	// Source 只参与运行期队列分流、百分比纠错与重置时刻可信度判断，不写入通用历史表。
	Source RefreshSource
}

// codexQuotaHistoryStateKey 只标识一个账号的一个主额度角色，用于当前周期比较缓存。
type codexQuotaHistoryStateKey struct {
	// AuthIndex 是 CPA OAuth Auth File 稳定账号键。
	AuthIndex string
	// WindowRole 只允许 primary/secondary；周期切换不会改变这个缓存键。
	WindowRole string
}

// codexQuotaHistoryCurrentState 是数据库恢复或 runner 接受后的当前周期单调比较状态。
type codexQuotaHistoryCurrentState struct {
	// Found 表示已经存在可比较的当前周期；false 允许下一份 observation 建立基线。
	Found bool
	// WindowSeconds 是当前周期的上游原始秒数，参与周期身份比较。
	WindowSeconds int64
	// ResetAtSource 标识周期结束时刻来自上游直接返回，还是根据倒计时推算。
	ResetAtSource string
	// ResetAt 是当前周期结束时刻；倒计时推算值只允许在有界容差内合并。
	ResetAt time.Time
	// HasTail 表示当前周期已经接受至少一个整数百分比状态。
	HasTail bool
	// RemainingPercent 是当前周期已接受的最低整数剩余百分比，范围 0–100。
	RemainingPercent int
	// LastObservedAt 是最近一份被接受 observation 的真实观察 instant。
	LastObservedAt time.Time
	// PendingIndex 指向 pending 切片中可继续合并的当前尾段；-1 表示尾段已落库或尚不存在。
	PendingIndex int
}

// codexQuotaHistoryWriter 抽象 repository 写入，字段注入仅供定向失败测试使用。
type codexQuotaHistoryWriter func(context.Context, *gorm.DB, []repositorydto.CodexMainQuotaObservation) error

// codexQuotaHistoryLoader 抽象 writer 状态恢复，保证缓存失效后数据库仍是最终事实。
type codexQuotaHistoryLoader func(context.Context, *gorm.DB, string, string) (repositorydto.CodexQuotaHistoryState, error)

// codexQuotaHistoryIdentityLister 批量读取活跃 Auth File，Header observation 只有通过后才能进入状态机。
type codexQuotaHistoryIdentityLister func(context.Context, *gorm.DB, []string) ([]entities.UsageIdentity, error)

// newCodexQuotaHistoryTimer 创建一次性 flush timer；返回 stop 函数便于 shutdown 释放资源。
func newCodexQuotaHistoryTimer(delay time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(delay)
	return timer.C, func() { timer.Stop() }
}

// runCodexQuotaHistoryRunner 串行拥有状态：Header 等待十秒聚合，低频可信查询到达时立即执行。
func (s *Service) runCodexQuotaHistoryRunner() {
	// done channel 必须只由 runner 关闭，StopRefreshTasks 才能确保数据库关闭前不再有 history 写入。
	defer close(s.codexQuotaHistoryDoneCh)
	// 当前状态只保存每个账号角色的最新周期；旧周期待写段由 pending 独立保存。
	current := make(map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState)

	for {
		select {
		case <-s.codexQuotaHistoryHeaderWake:
			// 旧 Header wake 可能对应已被可信来源丢弃的队列，无数据时不启动空窗口。
			if len(s.codexQuotaHistoryHeaderQueue) == 0 {
				continue
			}
			// Header 第一条只启动固定窗口；可信来源可以中断等待并替代同账号角色的待写 Header。
			timerC, stopTimer := s.codexQuotaHistoryNewTimer(s.codexQuotaHistoryFlushInterval)
			select {
			case <-timerC:
				stopTimer()
				// 通知只负责唤醒；到点后原子固定两条队列，避免遗漏尚未发布通知的可信输入。
				s.processPreferredCodexQuotaHistoryInputs(current)
			case <-s.codexQuotaHistoryTrustedWake:
				// 可信来源没有人为等待；先写可信事实，再写未被同账号角色覆盖的 Header。
				stopTimer()
				s.processPreferredCodexQuotaHistoryInputs(current)
			case <-s.codexQuotaHistoryStopCh:
				// shutdown 不再等待剩余窗口，封住新投递后直接处理队列中已经接收的数据。
				stopTimer()
				s.flushQueuedCodexQuotaHistoryOnShutdown(current)
				return
			}
		case <-s.codexQuotaHistoryTrustedWake:
			// 空闲 runner 收到可信来源后同样立即分流两类事实，不创建十秒 timer。
			s.processPreferredCodexQuotaHistoryInputs(current)
		case <-s.codexQuotaHistoryStopCh:
			// 空闲时收到 shutdown，同样处理 stop 之前已成功入队的全部元素。
			s.flushQueuedCodexQuotaHistoryOnShutdown(current)
			return
		}
	}
}

// takeQueuedCodexQuotaHistoryInputs 在生产者短锁下固定两条来源队列边界，并清除只属于本批的通知。
func (s *Service) takeQueuedCodexQuotaHistoryInputs(includeHeader bool, includeTrusted bool) []codexQuotaHistoryInput {
	s.codexQuotaHistoryMu.Lock()
	headerCount := 0
	trustedCount := 0
	if includeHeader {
		headerCount = len(s.codexQuotaHistoryHeaderQueue)
		select {
		case <-s.codexQuotaHistoryHeaderWake:
		default:
		}
	}
	if includeTrusted {
		trustedCount = len(s.codexQuotaHistoryTrustedQueue)
		select {
		case <-s.codexQuotaHistoryTrustedWake:
		default:
		}
	}
	s.codexQuotaHistoryMu.Unlock()

	// runner 是两条 channel 的唯一消费者；这里仅按固定边界复制，来源优先级由调用方决定。
	inputs := make([]codexQuotaHistoryInput, 0, headerCount+trustedCount)
	for range headerCount {
		inputs = append(inputs, <-s.codexQuotaHistoryHeaderQueue)
	}
	for range trustedCount {
		inputs = append(inputs, <-s.codexQuotaHistoryTrustedQueue)
	}
	return inputs
}

// processPreferredCodexQuotaHistoryInputs 固定两条队列，并让可信事实先于未被覆盖的 Header 独立写入。
func (s *Service) processPreferredCodexQuotaHistoryInputs(current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState) {
	inputs := s.takeQueuedCodexQuotaHistoryInputs(true, true)
	candidates := codexQuotaHistoryCandidates(inputs)
	trusted, remainingHeaders := splitPreferredCodexQuotaHistoryCandidates(candidates)
	// 可信来源必须先完成 writer 调用；无关 Header 随后单独写入，不能反向延迟可信校准。
	s.processCodexQuotaHistoryCandidateBatch(current, trusted)
	s.processCodexQuotaHistoryCandidateBatch(current, remainingHeaders)
}

// splitPreferredCodexQuotaHistoryCandidates 只丢弃被同账号、同角色可信事实替代的 Header 候选。
func splitPreferredCodexQuotaHistoryCandidates(candidates []codexQuotaHistoryCandidate) ([]codexQuotaHistoryCandidate, []codexQuotaHistoryCandidate) {
	trusted := make([]codexQuotaHistoryCandidate, 0, len(candidates))
	trustedKeys := make(map[codexQuotaHistoryStateKey]struct{})
	for _, candidate := range candidates {
		if !codexQuotaHistorySourceIsAuthoritative(candidate.Source) {
			continue
		}
		trusted = append(trusted, candidate)
		key := codexQuotaHistoryCandidateStateKey(candidate)
		if key.AuthIndex != "" && (key.WindowRole == "primary" || key.WindowRole == "secondary") {
			trustedKeys[key] = struct{}{}
		}
	}
	if len(trusted) == 0 {
		return nil, candidates
	}

	remainingHeaders := make([]codexQuotaHistoryCandidate, 0, len(candidates)-len(trusted))
	for _, candidate := range candidates {
		if codexQuotaHistorySourceIsAuthoritative(candidate.Source) {
			continue
		}
		// 可信查询只覆盖它实际返回的账号角色；其它账号或同账号另一角色仍保留本轮 Header。
		if _, superseded := trustedKeys[codexQuotaHistoryCandidateStateKey(candidate)]; superseded {
			continue
		}
		remainingHeaders = append(remainingHeaders, candidate)
	}
	return trusted, remainingHeaders
}

// codexQuotaHistoryCandidateStateKey 统一规范 runner 分流与状态缓存使用的账号角色键。
func codexQuotaHistoryCandidateStateKey(candidate codexQuotaHistoryCandidate) codexQuotaHistoryStateKey {
	return codexQuotaHistoryStateKey{
		AuthIndex:  strings.TrimSpace(candidate.Observation.AuthIndex),
		WindowRole: strings.ToLower(strings.TrimSpace(candidate.Observation.WindowRole)),
	}
}

// processCodexQuotaHistoryCandidateBatch 只处理同一来源类别，并按账号、窗口和观察时间恢复内部顺序。
func (s *Service) processCodexQuotaHistoryCandidateBatch(current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState, candidates []codexQuotaHistoryCandidate) {
	if len(candidates) == 0 {
		return
	}
	// 同一批某个账号窗口恢复失败后直接跳过余下候选；下一批会重新创建集合并自然重试。
	failedRecoveries := make(map[codexQuotaHistoryStateKey]struct{})
	verified := s.verifyCodexQuotaHistoryCandidates(candidates)
	// Redis inbox ID 和主动刷新完成顺序不保证等于各自观察时间；同类来源内部先恢复真实时序。
	sort.SliceStable(verified, func(left int, right int) bool {
		leftObservation := verified[left].Observation
		rightObservation := verified[right].Observation
		leftAuthIndex := strings.TrimSpace(leftObservation.AuthIndex)
		rightAuthIndex := strings.TrimSpace(rightObservation.AuthIndex)
		if leftAuthIndex != rightAuthIndex {
			return leftAuthIndex < rightAuthIndex
		}
		leftWindowRole := strings.ToLower(strings.TrimSpace(leftObservation.WindowRole))
		rightWindowRole := strings.ToLower(strings.TrimSpace(rightObservation.WindowRole))
		if leftWindowRole != rightWindowRole {
			return leftWindowRole < rightWindowRole
		}
		return leftObservation.LastObservedAt.Before(rightObservation.LastObservedAt)
	})
	pending := make([]repositorydto.CodexMainQuotaObservation, 0, len(verified))
	for _, candidate := range verified {
		// 每条 observation 按账号角色恢复/比较；恢复失败只放弃本批同状态键候选。
		s.mergeCodexQuotaHistoryObservation(current, failedRecoveries, &pending, candidate.Observation, candidate.Source)
	}
	s.flushCodexQuotaHistory(current, &pending, codexQuotaHistoryCandidateSourceSummary(verified))
}

// codexQuotaHistoryCandidateSourceSummary 为一个来源独立 writer 批次生成稳定、去重的诊断列表。
func codexQuotaHistoryCandidateSourceSummary(candidates []codexQuotaHistoryCandidate) string {
	sourceSet := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		sourceSet[refreshSourceLogValue(candidate.Source)] = struct{}{}
	}
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return strings.Join(sources, ",")
}

// codexQuotaHistoryCandidates 复制最小 observation，并让输入批次退出作用域后释放完整快照引用。
func codexQuotaHistoryCandidates(inputs []codexQuotaHistoryInput) []codexQuotaHistoryCandidate {
	// 每个输入最多两个主窗口，容量上限避免正常 Primary/Secondary 批次重复扩容。
	candidates := make([]codexQuotaHistoryCandidate, 0, len(inputs)*2)
	for index := range inputs {
		input := inputs[index]
		if input.Snapshot != nil {
			// Header 快照只能走未验证分支；复制 DTO 值后不再保存 snapshot 指针。
			for _, observation := range input.Snapshot.MainQuotaObservations {
				candidates = append(candidates, codexQuotaHistoryCandidate{Observation: observation, Source: input.Source})
			}
		}
		// 主动查询切片所有权已经转交 runner，按输入顺序复制值并保留验证标记。
		for _, observation := range input.Observations {
			candidates = append(candidates, codexQuotaHistoryCandidate{
				Observation:      observation,
				IdentityVerified: input.IdentityVerified,
				Source:           input.Source,
			})
		}
		// 显式清空局部引用，便于长批次处理期间尽早解除完整快照可达性。
		inputs[index].Snapshot = nil
		inputs[index].Observations = nil
	}
	return candidates
}

// verifyCodexQuotaHistoryCandidates 批量确认 Header 来源属于活跃 Codex Auth File。
func (s *Service) verifyCodexQuotaHistoryCandidates(candidates []codexQuotaHistoryCandidate) []codexQuotaHistoryCandidate {
	if len(candidates) == 0 {
		return nil
	}
	// 未验证集合只收集非空 auth_index；主动查询已由 Check 的活跃身份读取完成验证。
	authIndexes := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		if candidate.IdentityVerified {
			continue
		}
		authIndex := strings.TrimSpace(candidate.Observation.AuthIndex)
		if authIndex == "" {
			continue
		}
		if _, exists := seen[authIndex]; exists {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
	}

	// verified 保存真正的 Codex Auth File；查询失败时不猜测身份，主动已验证候选仍可继续。
	verifiedAuthIndexes := make(map[string]struct{}, len(authIndexes))
	if len(authIndexes) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), codexQuotaHistoryDatabaseTimeout)
		identities, err := s.codexQuotaHistoryListIdentities(ctx, s.db, authIndexes)
		cancel()
		if err != nil {
			logrus.WithError(err).Warn("codex quota history identity verification failed")
		} else {
			for _, identity := range identities {
				// AuthFile 条件由 repository 保证；这里再限制真实类型为 Codex，排除 provider 文本误判。
				if !usageHeaderIdentityIsCodex(identity) {
					continue
				}
				authIndex := strings.TrimSpace(identity.Identity)
				if authIndex != "" {
					verifiedAuthIndexes[authIndex] = struct{}{}
				}
			}
		}
	}

	// 保持原队列顺序过滤，确保同一账号的百分比状态按真实观察顺序进入单调状态机。
	verified := make([]codexQuotaHistoryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.IdentityVerified {
			verified = append(verified, candidate)
			continue
		}
		if _, ok := verifiedAuthIndexes[strings.TrimSpace(candidate.Observation.AuthIndex)]; ok {
			verified = append(verified, candidate)
		}
	}
	return verified
}

// mergeCodexQuotaHistoryObservation 把单条候选应用到当前周期缓存和独立 pending 容器。
func (s *Service) mergeCodexQuotaHistoryObservation(current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState, failedRecoveries map[codexQuotaHistoryStateKey]struct{}, pending *[]repositorydto.CodexMainQuotaObservation, observation repositorydto.CodexMainQuotaObservation, source RefreshSource) bool {
	// runner 只接受构造层已经规范的两个角色；异常 DTO 留给 repository 前先在内存边界拒绝。
	key := codexQuotaHistoryStateKey{
		AuthIndex:  strings.TrimSpace(observation.AuthIndex),
		WindowRole: strings.ToLower(strings.TrimSpace(observation.WindowRole)),
	}
	if key.AuthIndex == "" || (key.WindowRole != "primary" && key.WindowRole != "secondary") {
		return false
	}
	observation.AuthIndex = key.AuthIndex
	observation.WindowRole = key.WindowRole
	// 三种专门额度接口的可信度只在本次运行期传给 repository，实体和通用历史表不保存来源。
	observation.Authoritative = codexQuotaHistorySourceIsAuthoritative(source)
	// 时间计算、整数百分比和累计次数先做轻量校验，避免异常内部调用破坏缓存状态。
	if observation.WindowSeconds <= 0 || observation.WindowSeconds > math.MaxInt64/int64(time.Second) ||
		observation.ResetAt.IsZero() || observation.RemainingPercent < 0 || observation.RemainingPercent > 100 ||
		observation.ObservationCount < 1 {
		return false
	}

	// 缓存首次命中或写失败失效后必须从 writer 恢复当前周期，内存不能无条件建立新基线。
	state, loaded := current[key]
	if !loaded {
		if _, failed := failedRecoveries[key]; failed {
			return false
		}
		recovered, err := s.loadCodexQuotaHistoryCurrentState(key)
		if err != nil {
			failedRecoveries[key] = struct{}{}
			logrus.WithError(err).WithFields(logrus.Fields{
				"auth_index":  key.AuthIndex,
				"window_role": key.WindowRole,
			}).Warn("codex quota history state recovery failed")
			return false
		}
		state = recovered
		current[key] = state
	}

	// 所有候选必须有可排序时间；repository 仍会在事务内做最终完整校验。
	if observation.FirstObservedAt.IsZero() || observation.LastObservedAt.IsZero() || observation.FirstObservedAt.After(observation.LastObservedAt) {
		return false
	}
	// 当前周期存在时，先区分同周期、未来新周期和旧周期迟到三种路径。
	if state.Found {
		// 窗口秒数变化优先结束旧周期；Weekly→5h 时不能再用更远的 Weekly reset 阻止切换。
		if state.WindowSeconds != observation.WindowSeconds {
			if state.HasTail && observation.LastObservedAt.Before(state.LastObservedAt) {
				return false
			}
			state = codexQuotaHistoryStateFromObservation(observation)
			*pending = append(*pending, observation)
			state.PendingIndex = len(*pending) - 1
			current[key] = state
			return true
		}

		sameCycle := codexQuotaHistorySameCycle(state, observation)
		if !sameCycle {
			// 相同窗口内，observation 时间早于当前尾段或 reset 早于当前周期都属于迟到旧事实。
			if (state.HasTail && observation.LastObservedAt.Before(state.LastObservedAt)) || observation.ResetAt.Before(state.ResetAt) {
				return false
			}
			// 同窗口 reset 超出两分钟容差后建立新周期，并允许百分比回到高值。
			state = codexQuotaHistoryStateFromObservation(observation)
			*pending = append(*pending, observation)
			state.PendingIndex = len(*pending) - 1
			current[key] = state
			return true
		}

		// 同周期乱序 observation 不允许重排段；可信主动查询也不能用旧事实反向校准边界。
		if state.HasTail && observation.LastObservedAt.Before(state.LastObservedAt) {
			return false
		}
		// 可信来源可校准两分钟容差内的任意 Header 边界；普通 Header 仍只允许 relative→absolute 升级。
		boundaryUpgraded := state.ResetAtSource == "relative" && observation.ResetAtSource == "absolute"
		trustedBoundaryCalibration := observation.Authoritative &&
			(state.ResetAtSource != observation.ResetAtSource || !state.ResetAt.Equal(observation.ResetAt))
		boundaryUpdated := boundaryUpgraded || trustedBoundaryCalibration
		if boundaryUpdated {
			state.ResetAtSource = observation.ResetAtSource
			state.ResetAt = observation.ResetAt
		}
		// 同周期可信百分比回升不是新状态段，而是对已接受错误低值尾段的事务纠错。
		if state.HasTail && observation.RemainingPercent > state.RemainingPercent && observation.Authoritative {
			*pending = append(*pending, observation)
			state.RemainingPercent = observation.RemainingPercent
			state.LastObservedAt = observation.LastObservedAt
			state.PendingIndex = len(*pending) - 1
			current[key] = state
			return true
		}
		if state.HasTail && observation.LastObservedAt.Equal(state.LastObservedAt) {
			if observation.RemainingPercent != state.RemainingPercent {
				logrus.WithFields(logrus.Fields{
					"auth_index":       key.AuthIndex,
					"window_role":      key.WindowRole,
					"last_observed_at": observation.LastObservedAt,
				}).Warn("codex quota history observation conflicts at the same timestamp")
			}
			if boundaryUpdated {
				*pending = append(*pending, observation)
				current[key] = state
				return true
			}
			return false
		}
		// Header 或其它非可信来源的同周期回升仍违反单调不增不变量，直接忽略且不推进时间。
		if state.HasTail && observation.RemainingPercent > state.RemainingPercent {
			return false
		}
		// 相同整数百分比优先合并当前 pending 尾段；已落库尾段则新建一次增量 UPDATE observation。
		if state.HasTail && observation.RemainingPercent == state.RemainingPercent {
			if state.PendingIndex >= 0 && state.PendingIndex < len(*pending) {
				segment := &(*pending)[state.PendingIndex]
				// 同一待写尾段的边界被允许更新时，把最终值带进 repository，不能只更新内存状态。
				if boundaryUpdated {
					segment.ResetAtSource = observation.ResetAtSource
					segment.ResetAt = observation.ResetAt
				}
				segment.LastObservedAt = observation.LastObservedAt
				segment.ObservationCount += observation.ObservationCount
			} else {
				*pending = append(*pending, observation)
				state.PendingIndex = len(*pending) - 1
			}
			state.LastObservedAt = observation.LastObservedAt
			current[key] = state
			return true
		}
	}

	// 没有当前周期、当前周期没有尾段，或同周期百分比真实下降时追加新状态段。
	*pending = append(*pending, observation)
	if !state.Found {
		state = codexQuotaHistoryStateFromObservation(observation)
	} else {
		state.HasTail = true
		state.RemainingPercent = observation.RemainingPercent
		state.LastObservedAt = observation.LastObservedAt
	}
	state.PendingIndex = len(*pending) - 1
	current[key] = state
	return true
}

// codexQuotaHistorySourceIsAuthoritative 只信任实际调用专门额度接口且低频执行的三种刷新入口。
func codexQuotaHistorySourceIsAuthoritative(source RefreshSource) bool {
	switch source {
	case RefreshSourceManual, RefreshSourceScheduled, RefreshSourceInspection:
		return true
	default:
		return false
	}
}

// loadCodexQuotaHistoryCurrentState 使用有界 writer 读取恢复一个账号角色的当前周期和尾段。
func (s *Service) loadCodexQuotaHistoryCurrentState(key codexQuotaHistoryStateKey) (codexQuotaHistoryCurrentState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), codexQuotaHistoryDatabaseTimeout)
	defer cancel()
	recovered, err := s.codexQuotaHistoryLoad(ctx, s.db, key.AuthIndex, key.WindowRole)
	if err != nil {
		return codexQuotaHistoryCurrentState{}, err
	}
	if !recovered.Found {
		return codexQuotaHistoryCurrentState{PendingIndex: -1}, nil
	}
	return codexQuotaHistoryCurrentState{
		Found:            true,
		WindowSeconds:    recovered.WindowSeconds,
		ResetAtSource:    recovered.ResetAtSource,
		ResetAt:          recovered.ResetAt,
		HasTail:          recovered.HasTail,
		RemainingPercent: recovered.TailRemainingPercent,
		LastObservedAt:   recovered.TailLastObservedAt,
		PendingIndex:     -1,
	}, nil
}

// codexQuotaHistoryStateFromObservation 建立新周期或空缓存的首个当前状态。
func codexQuotaHistoryStateFromObservation(observation repositorydto.CodexMainQuotaObservation) codexQuotaHistoryCurrentState {
	return codexQuotaHistoryCurrentState{
		Found:            true,
		WindowSeconds:    observation.WindowSeconds,
		ResetAtSource:    observation.ResetAtSource,
		ResetAt:          observation.ResetAt,
		HasTail:          true,
		RemainingPercent: observation.RemainingPercent,
		LastObservedAt:   observation.LastObservedAt,
		PendingIndex:     -1,
	}
}

// codexQuotaHistorySameCycle 用固定两分钟容差判断 observation 是否属于内存当前周期。
func codexQuotaHistorySameCycle(state codexQuotaHistoryCurrentState, observation repositorydto.CodexMainQuotaObservation) bool {
	if state.WindowSeconds != observation.WindowSeconds {
		return false
	}
	return codexQuotaHistoryTimeDistance(state.ResetAt, observation.ResetAt) <= codexQuotaHistoryResetTolerance
}

// codexQuotaHistoryTimeDistance 返回两个 instant 的非负绝对距离。
func codexQuotaHistoryTimeDistance(left time.Time, right time.Time) time.Duration {
	distance := left.Sub(right)
	if distance < 0 {
		return -distance
	}
	return distance
}

// flushCodexQuotaHistory 用单个两秒 context 写入当前 pending，并按结果维护缓存可信度。
func (s *Service) flushCodexQuotaHistory(current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState, pending *[]repositorydto.CodexMainQuotaObservation, sources string) {
	if len(*pending) == 0 {
		return
	}
	// 跨账号保持接收顺序已足够；额外稳定排序只确保同账号角色严格按观察时间进入 repository。
	sort.SliceStable(*pending, func(left int, right int) bool {
		leftObservation := (*pending)[left]
		rightObservation := (*pending)[right]
		if leftObservation.AuthIndex != rightObservation.AuthIndex {
			return leftObservation.AuthIndex < rightObservation.AuthIndex
		}
		if leftObservation.WindowRole != rightObservation.WindowRole {
			return leftObservation.WindowRole < rightObservation.WindowRole
		}
		return leftObservation.FirstObservedAt.Before(rightObservation.FirstObservedAt)
	})
	ctx, cancel := context.WithTimeout(context.Background(), codexQuotaHistoryDatabaseTimeout)
	err := s.codexQuotaHistoryWrite(ctx, s.db, *pending)
	cancel()
	if err != nil {
		// best-effort 允许丢失本批新鲜度；清空 pending 避免部分提交批次重试造成 count 重复。
		logrus.WithError(err).WithFields(logrus.Fields{
			"observation_count": len(*pending),
			"sources":           sources,
		}).Warn("codex quota history flush failed")
		for _, observation := range *pending {
			// 受影响账号角色全部失效，下一份 observation 必须从 writer 数据库重新加载事实。
			delete(current, codexQuotaHistoryStateKey{AuthIndex: observation.AuthIndex, WindowRole: observation.WindowRole})
		}
		*pending = (*pending)[:0]
		return
	}
	// 写入成功后保留当前周期/百分比缓存，但所有尾段都不再指向已清空 pending。
	for key, state := range current {
		state.PendingIndex = -1
		current[key] = state
	}
	*pending = (*pending)[:0]
}

// flushQueuedCodexQuotaHistoryOnShutdown 固定关闭时的剩余数量，并保持与正常批次相同的处理规则。
func (s *Service) flushQueuedCodexQuotaHistoryOnShutdown(current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState) {
	s.processPreferredCodexQuotaHistoryInputs(current)
}

// tryAppendCodexQuotaHistorySnapshot 把同一不可变 Header 快照指针非阻塞投递到独立 history 队列。
func (s *Service) tryAppendCodexQuotaHistorySnapshot(snapshot *UsageHeaderSnapshot) bool {
	if snapshot == nil || len(snapshot.MainQuotaObservations) == 0 {
		return true
	}
	return s.tryAppendCodexQuotaHistoryInput(codexQuotaHistoryInput{
		Snapshot: snapshot,
		Source:   RefreshSourceUsageHeader,
	})
}

// tryAppendCodexQuotaHistoryObservations 投递主动查询已经验证身份的主额度 observation 切片。
func (s *Service) tryAppendCodexQuotaHistoryObservations(observations []repositorydto.CodexMainQuotaObservation, source RefreshSource) bool {
	if len(observations) == 0 {
		return true
	}
	return s.tryAppendCodexQuotaHistoryInput(codexQuotaHistoryInput{
		Observations:     observations,
		IdentityVerified: true,
		Source:           source,
	})
}

func refreshSourceLogValue(source RefreshSource) string {
	value := strings.TrimSpace(string(source))
	if value == "" {
		return "direct_check"
	}
	return value
}

// tryAppendCodexQuotaHistoryInput 在短锁内同时检查关闭状态和执行非阻塞发送，避免 send/stop 竞态。
func (s *Service) tryAppendCodexQuotaHistoryInput(input codexQuotaHistoryInput) bool {
	if s == nil {
		return false
	}
	s.codexQuotaHistoryMu.Lock()
	defer s.codexQuotaHistoryMu.Unlock()
	if s.codexQuotaHistoryClosing {
		return false
	}
	queue := s.codexQuotaHistoryHeaderQueue
	wake := s.codexQuotaHistoryHeaderWake
	authoritative := codexQuotaHistorySourceIsAuthoritative(input.Source)
	if authoritative {
		queue = s.codexQuotaHistoryTrustedQueue
		wake = s.codexQuotaHistoryTrustedWake
	}
	select {
	case queue <- input:
		// 两条通知都只表达“对应队列非空”；可信通知无等待，Header 通知开启十秒窗口。
		select {
		case wake <- struct{}{}:
		default:
		}
		return true
	default:
		return false
	}
}

// stopCodexQuotaHistoryRunner 封住新投递并等待 runner 的两秒 best-effort flush 完成。
func (s *Service) stopCodexQuotaHistoryRunner() {
	if s == nil {
		return
	}
	s.codexQuotaHistoryCloseOnce.Do(func() {
		// 关闭标记和 stop channel 在同一临界区发布，生产者不会向已停止 runner 遗留新元素。
		s.codexQuotaHistoryMu.Lock()
		s.codexQuotaHistoryClosing = true
		close(s.codexQuotaHistoryStopCh)
		s.codexQuotaHistoryMu.Unlock()
	})
	<-s.codexQuotaHistoryDoneCh
}
