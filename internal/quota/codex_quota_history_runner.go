package quota

import (
	"context"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

const (
	// codexQuotaHistoryFlushInterval 是首条数据入队后、固定本批队列边界前的一分钟默认等待时长。
	codexQuotaHistoryFlushInterval = time.Minute
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
	// LastMaterializedAt 是本进程最近成功写入该账号角色的墙钟时间，只用于五分钟稳定采样。
	LastMaterializedAt time.Time
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

// takeQueuedCodexQuotaHistoryInputs 在生产者短锁下固定两条来源队列的本批数量，并清除对应通知。
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

	// 锁外只读取已经固定的数量，避免 usage 生产者等待 runner 搬运最多 1024 条 Header。
	inputs := make([]codexQuotaHistoryInput, 0, headerCount+trustedCount)
	for range headerCount {
		inputs = append(inputs, <-s.codexQuotaHistoryHeaderQueue)
	}
	for range trustedCount {
		inputs = append(inputs, <-s.codexQuotaHistoryTrustedQueue)
	}
	return inputs
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
	accepted := false
	if authoritative {
		// 可信主动查询保持原有低频独立队列语义；本次只调整 Header 溢出策略。
		select {
		case queue <- input:
			accepted = true
		default:
		}
	} else {
		// Header 队列满时直接丢队头并保留后到数据；真实观察时间排序由 runner 批次统一完成。
		accepted = tryAppendCodexQuotaHistoryHeaderInput(queue, input)
	}
	if !accepted {
		return false
	}
	// 两条通知都只表达“对应队列非空”；可信通知无等待，Header 通知开启一分钟窗口。
	select {
	case wake <- struct{}{}:
	default:
	}
	return true
}

// tryAppendCodexQuotaHistoryHeaderInput 在调用方锁内 O(1) 写入；满载时 FIFO 淘汰一个队头。
func tryAppendCodexQuotaHistoryHeaderInput(queue chan codexQuotaHistoryInput, input codexQuotaHistoryInput) bool {
	if cap(queue) == 0 {
		return false
	}
	select {
	case queue <- input:
		return true
	default:
	}
	// runner 可能已经在首次发送失败后取走队头；此时无需再丢第二条。
	select {
	case <-queue:
	default:
	}
	// 所有生产者由同一把锁串行化，runner 只会继续取数据，因此这里仍是非阻塞 O(1) 发送。
	select {
	case queue <- input:
		return true
	default:
		return false
	}
}

// stopCodexQuotaHistoryRunner 封住新投递并等待 runner 完成最后一次 best-effort 处理。
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
