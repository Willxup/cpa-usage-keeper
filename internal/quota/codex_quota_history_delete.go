package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

type codexQuotaHistoryDeleteRequest struct {
	ctx       context.Context
	provider  string
	authIndex string
	cycleID   int64
	result    chan error
}

// DeleteCodexQuotaHistoryCycle 由唯一采样器串行执行，成功返回前数据库与内存已同步。
func (s *Service) DeleteCodexQuotaHistoryCycle(ctx context.Context, authIndex string, cycleID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("delete codex quota cycle: service database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" || cycleID <= 0 {
		return fmt.Errorf("%w: auth_index and positive cycle_id are required", ErrValidation)
	}
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	provider := normalizeIdentityType(identity.Type)
	if provider != "codex" && provider != "claude" {
		return ErrUnsupportedType
	}
	request := codexQuotaHistoryDeleteRequest{ctx: ctx, provider: provider, authIndex: authIndex, cycleID: cycleID, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.codexQuotaHistoryStopCh:
		return fmt.Errorf("quota history runner is stopped")
	case s.codexQuotaHistoryDelete <- request:
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.codexQuotaHistoryDoneCh:
		return fmt.Errorf("quota history runner is stopped")
	}
}

func (s *Service) deleteCodexQuotaHistoryCycle(state *codexQuotaHistoryRunnerState, request codexQuotaHistoryDeleteRequest) {
	ctx, cancel := context.WithTimeout(request.ctx, codexQuotaHistoryDatabaseTimeout)
	defer cancel()
	cycle, err := repository.DeleteQuotaCycle(ctx, s.db, request.provider, request.authIndex, request.cycleID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	if err == nil {
		role, _ := repositorydto.QuotaWindowRole(cycle.Provider, cycle.QuotaKey)
		key := codexQuotaHistoryStateKey{Provider: cycle.Provider, QuotaKey: cycle.QuotaKey, AuthIndex: cycle.AuthIndex, WindowRole: role}
		// writer 可能无错误忽略某条观察，因此分别核对内存状态本身的归属。
		if current, ok := state.Current[key]; ok && current.Found && cycle.MatchesObservation(repositorydto.CodexMainQuotaObservation{
			Provider: key.Provider, QuotaKey: key.QuotaKey, AuthIndex: key.AuthIndex, WindowRole: key.WindowRole, WindowSeconds: current.WindowSeconds, ResetAt: current.ResetAt,
		}) {
			delete(state.Current, key)
		}
		if stable, ok := state.Stable[key]; ok && cycle.MatchesObservation(stable.Observation) {
			delete(state.Stable, key)
		}
		s.clearQueuedCodexQuotaHistoryCycle(cycle)
	}
	request.result <- err
}

// clearQueuedCodexQuotaHistoryCycle 只清理此刻已入队的匹配 Header，可信接口结果及其唤醒通知保持原样。
func (s *Service) clearQueuedCodexQuotaHistoryCycle(cycle repository.CodexQuotaCycleDeletion) {
	// 与生产者共用短锁；队列有固定容量，锁内不访问数据库，也不等待新采样。
	s.codexQuotaHistoryMu.Lock()
	defer s.codexQuotaHistoryMu.Unlock()
	queue := s.codexQuotaHistoryHeaderQueue
	for range len(queue) {
		input := <-queue
		remaining := make([]repositorydto.CodexMainQuotaObservation, 0, 2)
		retain := func(observations []repositorydto.CodexMainQuotaObservation) {
			for _, observation := range observations {
				if !cycle.MatchesObservation(observation) {
					remaining = append(remaining, observation)
				}
			}
		}
		if input.Snapshot != nil {
			retain(input.Snapshot.MainQuotaObservations)
		}
		retain(input.Observations)
		if len(remaining) > 0 {
			// Header 快照也供实时限额缓存使用，过滤只作用于 history 自己的新切片。
			input.Snapshot = nil
			input.Observations = remaining
			queue <- input
		}
	}
}
