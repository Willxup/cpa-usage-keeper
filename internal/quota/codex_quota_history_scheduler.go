package quota

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"github.com/sirupsen/logrus"
)

const (
	// codexQuotaHistoryHeartbeatInterval 让稳定百分比的写入频率与 Header/request 数量脱钩。
	codexQuotaHistoryHeartbeatInterval = 5 * time.Minute
)

// codexQuotaHistoryDeferredStable 只保存一个账号角色尚未物化的最新稳定尾段。
type codexQuotaHistoryDeferredStable struct {
	// Observation 合并相同整数百分比的首尾观察时间与次数。
	Observation repositorydto.CodexMainQuotaObservation
	// Immediate 表示可信校准或 reset 边界升级不能等待稳定心跳。
	Immediate bool
}

// codexQuotaHistoryRunnerState 由唯一 runner goroutine 串行持有，不需要额外锁或容量限制。
type codexQuotaHistoryRunnerState struct {
	// Current 保存每个账号角色最近接受的周期和百分比，用于减少重复数据库恢复。
	Current map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState
	// Stable 每个账号角色最多一条，保存五分钟内尚未写入的相同百分比累计。
	Stable map[codexQuotaHistoryStateKey]codexQuotaHistoryDeferredStable
}

// codexQuotaHistoryCohortKey 标识同一账号在同一次 Header/额度响应中返回的主窗口集合。
type codexQuotaHistoryCohortKey struct {
	AuthIndex      string
	LastObservedAt time.Time
}

func newCodexQuotaHistoryRunnerState() codexQuotaHistoryRunnerState {
	return codexQuotaHistoryRunnerState{
		Current: make(map[codexQuotaHistoryStateKey]codexQuotaHistoryCurrentState),
		Stable:  make(map[codexQuotaHistoryStateKey]codexQuotaHistoryDeferredStable),
	}
}

// runCodexQuotaHistoryRunner 串行处理一分钟 Header 窗口；可信主动查询仍立即触发同一条路径。
func (s *Service) runCodexQuotaHistoryRunner() {
	// done 只能由 runner 关闭，App 才能在数据库关闭前确认 history 已经停止。
	defer close(s.codexQuotaHistoryDoneCh)
	state := newCodexQuotaHistoryRunnerState()

	for {
		select {
		case <-s.codexQuotaHistoryHeaderWake:
			// 队列和 wake 必须在生产者同一把短锁下观察，避免把并发新数据误判为空通知。
			s.codexQuotaHistoryMu.Lock()
			headerQueued := len(s.codexQuotaHistoryHeaderQueue) > 0
			s.codexQuotaHistoryMu.Unlock()
			if !headerQueued {
				continue
			}
			// 第一条 Header 固定开启一分钟窗口，后续 Header 只进入同一个有界队列。
			timerC, stopTimer := s.codexQuotaHistoryNewTimer(s.codexQuotaHistoryFlushInterval)
			select {
			case <-timerC:
				stopTimer()
				// 一分钟到点后固定本批输入，后续 Header 自然进入下一批。
				s.processPreferredCodexQuotaHistoryInputs(&state, false)
			case <-s.codexQuotaHistoryTrustedWake:
				stopTimer()
				// 可信查询跳过 Header 等待，并覆盖同账号角色的待处理 Header。
				s.processPreferredCodexQuotaHistoryInputs(&state, false)
			case <-s.codexQuotaHistoryStopCh:
				stopTimer()
				// shutdown 只尽力处理已经接收的数据，不等待剩余的一分钟窗口。
				s.processPreferredCodexQuotaHistoryInputs(&state, true)
				return
			}
		case <-s.codexQuotaHistoryTrustedWake:
			// 空闲 runner 收到可信事实时立即处理，不创建 Header timer。
			s.processPreferredCodexQuotaHistoryInputs(&state, false)
		case <-s.codexQuotaHistoryStopCh:
			// 即使队列为空，shutdown 也会尽力物化内存中的稳定尾段。
			s.processPreferredCodexQuotaHistoryInputs(&state, true)
			return
		}
	}
}

// processPreferredCodexQuotaHistoryInputs 固定当前队列，并用一次 writer 调用处理可信事实与未被覆盖的 Header。
func (s *Service) processPreferredCodexQuotaHistoryInputs(state *codexQuotaHistoryRunnerState, shutdown bool) {
	// 取得队列后所有状态只由当前 goroutine 持有，生产者可以立即继续投递下一分钟的数据。
	inputs := s.takeQueuedCodexQuotaHistoryInputs(true, true)
	candidates := codexQuotaHistoryCandidates(inputs)
	trusted, headers := splitPreferredCodexQuotaHistoryCandidates(candidates)
	// 同账号角色 Header 已由可信事实替代；其它账号角色随后按观察时间排序，共享同一次 writer 调用。
	preferred := append(trusted, headers...)
	s.processCodexQuotaHistoryCandidateBatch(state, preferred, time.Now(), shutdown)
}

// processCodexQuotaHistoryCandidateBatch 完成批量身份确认、单调合并和一次低频物化。
func (s *Service) processCodexQuotaHistoryCandidateBatch(state *codexQuotaHistoryRunnerState, candidates []codexQuotaHistoryCandidate, now time.Time, flushAllStable bool) {
	verified := s.verifyCodexQuotaHistoryCandidates(candidates)
	// Redis inbox 与主动查询的完成顺序不等于观察时间；进入状态机前恢复确定顺序。
	sortCodexQuotaHistoryCandidates(verified)

	// recovery 失败只影响本批同一账号角色；下一批会重新从数据库尝试恢复。
	failedRecoveries := make(map[codexQuotaHistoryStateKey]struct{})
	materializedCohorts := make(map[codexQuotaHistoryCohortKey]struct{})
	pending := make([]repositorydto.CodexMainQuotaObservation, 0, len(verified))
	for _, candidate := range verified {
		if s.mergeCodexQuotaHistoryObservation(state, failedRecoveries, &pending, candidate, now) {
			materializedCohorts[codexQuotaHistoryCandidateCohortKey(candidate)] = struct{}{}
		}
	}

	// 任一窗口在本批发生变化时，同一 Header 中仍处于稳定降频的另一窗口也必须一起物化。
	for key, deferred := range state.Stable {
		cohort := codexQuotaHistoryCohortKey{AuthIndex: key.AuthIndex, LastObservedAt: deferred.Observation.LastObservedAt}
		if _, materialized := materializedCohorts[cohort]; materialized {
			deferred.Immediate = true
			state.Stable[key] = deferred
		}
	}

	// 每次 runner 被唤醒都顺带收集已经到五分钟的稳定尾段；shutdown 则收集全部尾段。
	stableKeys := s.appendDueCodexQuotaHistoryStable(state, &pending, now, flushAllStable)
	s.flushCodexQuotaHistory(state, pending, stableKeys, codexQuotaHistoryCandidateSourceSummary(verified), now)
}

// codexQuotaHistoryCandidateCohortKey 用账号和观察时刻关联同一次响应里的 Primary/Secondary。
func codexQuotaHistoryCandidateCohortKey(candidate codexQuotaHistoryCandidate) codexQuotaHistoryCohortKey {
	return codexQuotaHistoryCohortKey{
		AuthIndex:      strings.TrimSpace(candidate.Observation.AuthIndex),
		LastObservedAt: candidate.Observation.LastObservedAt,
	}
}

// sortCodexQuotaHistoryCandidates 保证同账号角色严格按真实观察时间进入单调状态机。
func sortCodexQuotaHistoryCandidates(candidates []codexQuotaHistoryCandidate) {
	sort.SliceStable(candidates, func(left int, right int) bool {
		leftKey := codexQuotaHistoryCandidateStateKey(candidates[left])
		rightKey := codexQuotaHistoryCandidateStateKey(candidates[right])
		if leftKey.AuthIndex != rightKey.AuthIndex {
			return leftKey.AuthIndex < rightKey.AuthIndex
		}
		if leftKey.WindowRole != rightKey.WindowRole {
			return leftKey.WindowRole < rightKey.WindowRole
		}
		return candidates[left].Observation.LastObservedAt.Before(candidates[right].Observation.LastObservedAt)
	})
}

// verifyCodexQuotaHistoryCandidates 一次查询确认 Header 属于活跃 Codex Auth File；可信查询已经在 Check 中确认身份。
func (s *Service) verifyCodexQuotaHistoryCandidates(candidates []codexQuotaHistoryCandidate) []codexQuotaHistoryCandidate {
	if len(candidates) == 0 {
		return nil
	}

	// authIndexes 去重后只产生一次 reader 查询；空账号不会进入数据库条件。
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

	verifiedAuthIndexes := make(map[string]struct{}, len(authIndexes))
	if len(authIndexes) > 0 {
		// 身份查询有独立短超时；失败时宁可留下采样缺口，也不猜测 Header 归属。
		ctx, cancel := context.WithTimeout(context.Background(), codexQuotaHistoryDatabaseTimeout)
		identities, err := s.codexQuotaHistoryListIdentities(ctx, s.db, authIndexes)
		cancel()
		if err != nil {
			logrus.WithError(err).Warn("codex quota history identity verification failed")
		} else {
			for _, identity := range identities {
				// repository 已限定 Auth File；这里再限定真实 Codex 类型，排除 provider 文本误判。
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

	// 保持候选原顺序；只有可信查询或本次查询确认的 Header 可以继续。
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

// mergeCodexQuotaHistoryObservation 把一条 observation 合并为语义变化或单个稳定尾段。
func (s *Service) mergeCodexQuotaHistoryObservation(state *codexQuotaHistoryRunnerState, failedRecoveries map[codexQuotaHistoryStateKey]struct{}, pending *[]repositorydto.CodexMainQuotaObservation, candidate codexQuotaHistoryCandidate, now time.Time) bool {
	observation := candidate.Observation
	key := codexQuotaHistoryCandidateStateKey(candidate)
	// runner 只接受构造层定义的两个主额度角色；repository 仍保留最终完整校验。
	if key.AuthIndex == "" || (key.WindowRole != "primary" && key.WindowRole != "secondary") {
		return false
	}
	observation.AuthIndex = key.AuthIndex
	observation.WindowRole = key.WindowRole
	observation.Authoritative = codexQuotaHistorySourceIsAuthoritative(candidate.Source)
	// 内部 DTO 必须满足基本范围与时间顺序，异常值不能推进内存状态。
	if observation.WindowSeconds <= 0 || observation.WindowSeconds > math.MaxInt64/int64(time.Second) ||
		observation.ResetAt.IsZero() || observation.RemainingPercent < 0 || observation.RemainingPercent > 100 ||
		observation.ObservationCount < 1 || observation.FirstObservedAt.IsZero() || observation.LastObservedAt.IsZero() ||
		observation.FirstObservedAt.After(observation.LastObservedAt) {
		return false
	}

	current, loaded := state.Current[key]
	if !loaded {
		// 同一批第一次恢复失败后不重复打数据库；下一批仍可自然重试。
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
		current = recovered
		// 已有数据库尾段视为本进程刚确认，避免服务重启后立刻产生无意义稳定 UPDATE。
		if current.Found && current.HasTail {
			current.LastMaterializedAt = now
		}
		state.Current[key] = current
	}

	if current.Found {
		// 窗口变化代表角色切换；先封住旧稳定尾段，再写新周期基线。
		if current.WindowSeconds != observation.WindowSeconds {
			if current.HasTail && observation.LastObservedAt.Before(current.LastObservedAt) {
				return false
			}
			s.appendDeferredCodexQuotaHistoryStable(state, key, pending)
			s.appendCodexQuotaHistoryChange(state, key, &current, pending, observation)
			return true
		}

		sameCycle := codexQuotaHistorySameCycle(current, observation)
		if !sameCycle {
			// 同窗口只有时间向前且 reset 不倒退时才能建立下一周期。
			if (current.HasTail && observation.LastObservedAt.Before(current.LastObservedAt)) || observation.ResetAt.Before(current.ResetAt) {
				return false
			}
			s.appendDeferredCodexQuotaHistoryStable(state, key, pending)
			s.appendCodexQuotaHistoryChange(state, key, &current, pending, observation)
			return true
		}

		// 同周期乱序事实不能反向推进百分比或 reset 边界。
		if current.HasTail && observation.LastObservedAt.Before(current.LastObservedAt) {
			return false
		}
		boundaryUpgraded := current.ResetAtSource == "relative" && observation.ResetAtSource == "absolute"
		trustedCalibration := observation.Authoritative &&
			(current.ResetAtSource != observation.ResetAtSource || !current.ResetAt.Equal(observation.ResetAt))
		boundaryUpdated := boundaryUpgraded || trustedCalibration
		if boundaryUpdated {
			current.ResetAtSource = observation.ResetAtSource
			current.ResetAt = observation.ResetAt
		}

		// 可信专门额度查询允许纠正错误低尾段；普通 Header 回升仍直接忽略。
		if current.HasTail && observation.RemainingPercent > current.RemainingPercent && observation.Authoritative {
			s.appendDeferredCodexQuotaHistoryStable(state, key, pending)
			s.appendCodexQuotaHistoryChange(state, key, &current, pending, observation)
			return true
		}
		if current.HasTail && observation.LastObservedAt.Equal(current.LastObservedAt) {
			if observation.RemainingPercent != current.RemainingPercent {
				logrus.WithFields(logrus.Fields{
					"auth_index":       key.AuthIndex,
					"window_role":      key.WindowRole,
					"last_observed_at": observation.LastObservedAt,
				}).Warn("codex quota history observation conflicts at the same timestamp")
			}
			// reset 校准仍需立即交给 repository；相同时间的百分比冲突不会改变当前尾段。
			if boundaryUpdated {
				// 先物化此前累计的稳定尾段，再应用同一时刻的边界校准，避免后续被 repository 视为重叠旧数据。
				s.appendDeferredCodexQuotaHistoryStable(state, key, pending)
				if current.PendingIndex >= 0 && current.PendingIndex < len(*pending) {
					// 尚未落库的同一尾段直接原地升级；后续同值观察会继续合并到这条 absolute 事实。
					calibrated := &(*pending)[current.PendingIndex]
					calibrated.ResetAtSource = observation.ResetAtSource
					calibrated.ResetAt = observation.ResetAt
					calibrated.ObservationCount += observation.ObservationCount
					calibrated.Authoritative = calibrated.Authoritative || observation.Authoritative
				} else {
					// 已落库尾段无法原地修改；追加校准后必须让 PendingIndex 指向它，不能再引用旧 relative 条目。
					observation.RemainingPercent = current.RemainingPercent
					*pending = append(*pending, observation)
					current.PendingIndex = len(*pending) - 1
				}
				state.Current[key] = current
				return true
			}
			return false
		}
		if current.HasTail && observation.RemainingPercent > current.RemainingPercent {
			return false
		}
		if current.HasTail && observation.RemainingPercent == current.RemainingPercent {
			// 新状态已在本批 pending 时直接合并；否则只保留一个五分钟稳定尾段。
			materialized := false
			if current.PendingIndex >= 0 && current.PendingIndex < len(*pending) {
				merged := &(*pending)[current.PendingIndex]
				if boundaryUpdated {
					merged.ResetAtSource = observation.ResetAtSource
					merged.ResetAt = observation.ResetAt
				}
				merged.LastObservedAt = observation.LastObservedAt
				merged.ObservationCount += observation.ObservationCount
				merged.Authoritative = merged.Authoritative || observation.Authoritative
				materialized = true
			} else {
				s.mergeDeferredCodexQuotaHistoryStable(state, key, observation, boundaryUpdated || observation.Authoritative)
			}
			current.LastObservedAt = observation.LastObservedAt
			state.Current[key] = current
			return materialized
		}
	}

	// 空状态、空尾段或同周期真实下降都是语义变化，必须在本批立即写入。
	s.appendDeferredCodexQuotaHistoryStable(state, key, pending)
	s.appendCodexQuotaHistoryChange(state, key, &current, pending, observation)
	return true
}

// appendCodexQuotaHistoryChange 把新周期、真实下降或可信纠错追加到本批，并推进当前比较状态。
func (s *Service) appendCodexQuotaHistoryChange(state *codexQuotaHistoryRunnerState, key codexQuotaHistoryStateKey, current *codexQuotaHistoryCurrentState, pending *[]repositorydto.CodexMainQuotaObservation, observation repositorydto.CodexMainQuotaObservation) {
	*pending = append(*pending, observation)
	if !current.Found || current.WindowSeconds != observation.WindowSeconds || !codexQuotaHistorySameCycle(*current, observation) {
		*current = codexQuotaHistoryStateFromObservation(observation)
	} else {
		current.HasTail = true
		current.RemainingPercent = observation.RemainingPercent
		current.LastObservedAt = observation.LastObservedAt
		current.ResetAtSource = observation.ResetAtSource
		current.ResetAt = observation.ResetAt
	}
	current.PendingIndex = len(*pending) - 1
	state.Current[key] = *current
}

// mergeDeferredCodexQuotaHistoryStable 合并同一整数百分比，不创建额外 timer 或每账号缓存队列。
func (s *Service) mergeDeferredCodexQuotaHistoryStable(state *codexQuotaHistoryRunnerState, key codexQuotaHistoryStateKey, observation repositorydto.CodexMainQuotaObservation, immediate bool) {
	deferred, exists := state.Stable[key]
	if !exists {
		state.Stable[key] = codexQuotaHistoryDeferredStable{Observation: observation, Immediate: immediate}
		return
	}
	// 第一观察时间保持不变；最后时间、累计次数和最终 reset 校准向前推进。
	deferred.Observation.LastObservedAt = observation.LastObservedAt
	deferred.Observation.ObservationCount += observation.ObservationCount
	deferred.Observation.Authoritative = deferred.Observation.Authoritative || observation.Authoritative
	if observation.Authoritative || deferred.Observation.ResetAtSource == "relative" && observation.ResetAtSource == "absolute" {
		deferred.Observation.ResetAtSource = observation.ResetAtSource
		deferred.Observation.ResetAt = observation.ResetAt
	}
	deferred.Immediate = deferred.Immediate || immediate
	state.Stable[key] = deferred
}

// appendDeferredCodexQuotaHistoryStable 在后续语义变化前先写旧稳定尾段，保持真实观察顺序。
func (s *Service) appendDeferredCodexQuotaHistoryStable(state *codexQuotaHistoryRunnerState, key codexQuotaHistoryStateKey, pending *[]repositorydto.CodexMainQuotaObservation) {
	deferred, exists := state.Stable[key]
	if !exists {
		return
	}
	*pending = append(*pending, deferred.Observation)
	delete(state.Stable, key)
}

// appendDueCodexQuotaHistoryStable 收集到期尾段；没有新 Header 时无需为了心跳单独唤醒数据库。
func (s *Service) appendDueCodexQuotaHistoryStable(state *codexQuotaHistoryRunnerState, pending *[]repositorydto.CodexMainQuotaObservation, now time.Time, all bool) []codexQuotaHistoryStateKey {
	dueCohorts := make(map[codexQuotaHistoryCohortKey]struct{})
	for key, deferred := range state.Stable {
		current := state.Current[key]
		due := deferred.Immediate || current.LastMaterializedAt.IsZero() || !now.Before(current.LastMaterializedAt.Add(s.codexQuotaHistoryHeartbeatInterval))
		if all || due {
			dueCohorts[codexQuotaHistoryCohortKey{AuthIndex: key.AuthIndex, LastObservedAt: deferred.Observation.LastObservedAt}] = struct{}{}
		}
	}
	// 一个窗口到期或被标记立即写入时，把同一响应中存在的另一个主窗口一起收集。
	keys := make([]codexQuotaHistoryStateKey, 0)
	for key, deferred := range state.Stable {
		cohort := codexQuotaHistoryCohortKey{AuthIndex: key.AuthIndex, LastObservedAt: deferred.Observation.LastObservedAt}
		if _, due := dueCohorts[cohort]; due {
			keys = append(keys, key)
		}
	}
	// map 遍历无序；账号和角色排序让测试、日志与 repository 输入保持稳定。
	sort.Slice(keys, func(left int, right int) bool {
		if keys[left].AuthIndex != keys[right].AuthIndex {
			return keys[left].AuthIndex < keys[right].AuthIndex
		}
		return keys[left].WindowRole < keys[right].WindowRole
	})
	for _, key := range keys {
		*pending = append(*pending, state.Stable[key].Observation)
	}
	return keys
}

// codexQuotaHistoryCandidateSourceSummary 为失败日志生成稳定、去重的来源列表。
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

// flushCodexQuotaHistory 用一次两秒 writer 调用物化本批；失败时丢弃本批并让下一份输入重新恢复数据库事实。
func (s *Service) flushCodexQuotaHistory(state *codexQuotaHistoryRunnerState, pending []repositorydto.CodexMainQuotaObservation, stableKeys []codexQuotaHistoryStateKey, sources string, now time.Time) {
	if len(pending) == 0 {
		return
	}
	// 同账号角色必须按观察时间进入 repository；不同账号仅使用稳定排序便于诊断。
	sort.SliceStable(pending, func(left int, right int) bool {
		if pending[left].AuthIndex != pending[right].AuthIndex {
			return pending[left].AuthIndex < pending[right].AuthIndex
		}
		if pending[left].WindowRole != pending[right].WindowRole {
			return pending[left].WindowRole < pending[right].WindowRole
		}
		return pending[left].FirstObservedAt.Before(pending[right].FirstObservedAt)
	})

	ctx, cancel := context.WithTimeout(context.Background(), codexQuotaHistoryDatabaseTimeout)
	err := s.codexQuotaHistoryWrite(ctx, s.db, pending)
	cancel()
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"observation_count": len(pending),
			"sources":           sources,
		}).Warn("codex quota history flush failed")
		// history 是低优先级采样：失败允许形成缺口，但不能用未提交内存状态继续推断。
		for _, observation := range pending {
			key := codexQuotaHistoryStateKey{AuthIndex: observation.AuthIndex, WindowRole: observation.WindowRole}
			delete(state.Current, key)
			delete(state.Stable, key)
		}
		return
	}

	// 成功后稳定尾段才可从内存删除；语义变化也同步刷新五分钟物化时钟。
	for _, key := range stableKeys {
		delete(state.Stable, key)
	}
	for _, observation := range pending {
		key := codexQuotaHistoryStateKey{AuthIndex: observation.AuthIndex, WindowRole: observation.WindowRole}
		current, exists := state.Current[key]
		if !exists {
			continue
		}
		current.LastMaterializedAt = now
		current.PendingIndex = -1
		state.Current[key] = current
	}
	// 本批其它状态也不能继续引用已经释放的 pending 切片。
	for key, current := range state.Current {
		current.PendingIndex = -1
		state.Current[key] = current
	}
}
