package ranking

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	ErrSyncInProgress = errors.New("ranking synchronization is already running")
	ErrInvalidProfile = errors.New("invalid ranking profile")
	ErrProfileLocked  = errors.New("ranking profile is immutable")
	ErrParticipation  = errors.New("ranking participation state does not allow this action")
)

const maxDisplayNameRunes = 16

type CenterAPI interface {
	Register(context.Context, RegistrationCommand) (ParticipantInfo, error)
	Self(context.Context, Credentials, time.Time) (SelfStatus, error)
	SubmitReport(context.Context, ReportCommand) (ReportReceipt, error)
	Delete(context.Context, DeleteCommand) (DeleteReceipt, error)
	Leaderboard(context.Context, LeaderboardPeriod, LeaderboardMetric) (Leaderboard, error)
	LeaderboardMetadata(context.Context) (LeaderboardMetadata, error)
}

type UsageAggregator interface {
	AggregateDay(context.Context, time.Time, time.Time) (Metrics, error)
	LatestEventID(context.Context, time.Time, time.Time) (int64, error)
}

type LocalStatus struct {
	Status                    Status     `json:"status"`
	DisplayName               string     `json:"display_name,omitempty"`
	AvatarID                  uint8      `json:"avatar_id,omitempty"`
	ParticipantID             string     `json:"participant_id,omitempty"`
	LastSuccessfulCompleteDay string     `json:"last_successful_complete_day,omitempty"`
	LastSuccessfulSyncAt      *time.Time `json:"last_successful_sync_at,omitempty"`
	LastAttemptAt             *time.Time `json:"last_attempt_at,omitempty"`
	LastError                 string     `json:"last_error,omitempty"`
}

type Service struct {
	store      *Store
	aggregator UsageAggregator
	center     CenterAPI
	readCache  *readCache
	now        func() time.Time
	random     io.Reader

	operationMu        sync.Mutex
	reconciled         bool
	lastDynamicDay     string
	lastDynamicEventID int64
}

type ServiceOption func(*Service)

func WithClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		if clock != nil {
			service.now = clock
		}
	}
}

func WithRandom(random io.Reader) ServiceOption {
	return func(service *Service) {
		if random != nil {
			service.random = random
		}
	}
}

func NewService(store *Store, aggregator UsageAggregator, center CenterAPI, options ...ServiceOption) (*Service, error) {
	if store == nil || store.db == nil {
		return nil, fmt.Errorf("ranking store is required")
	}
	if aggregator == nil {
		return nil, fmt.Errorf("ranking aggregator is required")
	}
	if center == nil {
		return nil, fmt.Errorf("ranking center client is required")
	}
	service := &Service{store: store, aggregator: aggregator, center: center, readCache: newReadCache(), now: time.Now, random: rand.Reader}
	for _, option := range options {
		option(service)
	}
	return service, nil
}

func (s *Service) Status(ctx context.Context) (LocalStatus, error) {
	state, err := s.store.Load(ctx)
	if err != nil {
		return LocalStatus{}, err
	}
	return localStatus(state), nil
}

func (s *Service) Join(ctx context.Context, displayName string, avatarID uint8) (LocalStatus, error) {
	if !s.operationMu.TryLock() {
		return LocalStatus{}, ErrSyncInProgress
	}
	defer s.operationMu.Unlock()

	name, err := normalizeProfile(displayName, avatarID)
	if err != nil {
		return LocalStatus{}, err
	}
	state, err := s.store.Load(ctx)
	if err != nil {
		return LocalStatus{}, err
	}
	switch state.Status {
	case StatusDeleted:
		return localStatus(state), ErrDeletedState
	case StatusActive:
		if state.DisplayName != name || state.AvatarID != avatarID {
			return localStatus(state), ErrProfileLocked
		}
		return localStatus(state), nil
	case StatusPaused:
		if state.DisplayName != name || state.AvatarID != avatarID {
			return localStatus(state), ErrProfileLocked
		}
		return localStatus(state), nil
	case StatusJoining:
		if state.DisplayName != name || state.AvatarID != avatarID {
			return localStatus(state), ErrProfileLocked
		}
	case StatusDisabled:
		identity, err := GenerateIdentity(s.random)
		if err != nil {
			return LocalStatus{}, err
		}
		registrationID, err := newIdempotencyKey("reg_", s.random)
		if err != nil {
			return LocalStatus{}, err
		}
		state = State{
			Status: StatusJoining, PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey,
			RegistrationIdempotencyKey: registrationID, DisplayName: name, AvatarID: avatarID,
		}
		if err := s.store.Save(ctx, state); err != nil {
			return LocalStatus{}, err
		}
	default:
		return LocalStatus{}, ErrInvalidState
	}

	status, err := s.finishJoiningLocked(ctx, state, true)
	if err != nil {
		return status, err
	}
	return status, nil
}

// RunOnce 供后台调度使用；未参与或暂停时静默跳过，避免把正常空闲状态记录成任务错误。
func (s *Service) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx, false)
}

// SyncNow 供管理员手动触发；只有 active 状态才返回成功，避免把静默跳过误报为已上报。
func (s *Service) SyncNow(ctx context.Context) error {
	return s.runOnce(ctx, true)
}

func (s *Service) runOnce(ctx context.Context, requireActive bool) error {
	if !s.operationMu.TryLock() {
		return ErrSyncInProgress
	}
	defer s.operationMu.Unlock()

	state, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	switch state.Status {
	case StatusDisabled, StatusPaused, StatusDeleted:
		if requireActive {
			return ErrParticipation
		}
		return nil
	case StatusJoining:
		// 注册失败后只允许管理员手动重试，避免后台任务消耗中心注册限流。
		if requireActive {
			return ErrParticipation
		}
		return nil
	case StatusActive:
	default:
		return ErrInvalidState
	}
	reconciledBeforeSync := s.reconciled
	if !reconciledBeforeSync {
		state, err = s.reconcileLocked(ctx, state)
		if err != nil {
			return err
		}
		if state.Status == StatusDeleted {
			return ErrParticipantDeleted
		}
	}
	if err := s.syncActiveLocked(ctx, state); err != nil {
		return err
	}
	if !reconciledBeforeSync {
		return nil
	}
	// 常规轮次先完成用量同步，再用轻量 Self 校准中心状态；空闲实例也能收敛删除墓碑，且 Self 失败不会阻断本轮上报。
	state, err = s.reconcileLocked(ctx, state)
	if err != nil {
		return err
	}
	if state.Status == StatusDeleted {
		return ErrParticipantDeleted
	}
	return nil
}

func (s *Service) Pause(ctx context.Context) (LocalStatus, error) {
	if !s.operationMu.TryLock() {
		return LocalStatus{}, ErrSyncInProgress
	}
	defer s.operationMu.Unlock()

	state, err := s.store.Update(ctx, func(next *State) error {
		switch next.Status {
		case StatusPaused:
			return nil
		case StatusActive:
			next.Status = StatusPaused
			return nil
		case StatusDeleted:
			return ErrDeletedState
		default:
			return ErrParticipation
		}
	})
	if err != nil {
		return LocalStatus{}, err
	}
	return localStatus(state), nil
}

func (s *Service) Resume(ctx context.Context) (LocalStatus, error) {
	if !s.operationMu.TryLock() {
		return LocalStatus{}, ErrSyncInProgress
	}
	defer s.operationMu.Unlock()

	state, err := s.store.Update(ctx, func(next *State) error {
		switch next.Status {
		case StatusActive:
			return nil
		case StatusPaused:
			next.Status = StatusActive
			return nil
		case StatusDeleted:
			return ErrDeletedState
		default:
			return ErrParticipation
		}
	})
	if err != nil {
		return LocalStatus{}, err
	}
	return localStatus(state), nil
}

func (s *Service) Exit(ctx context.Context) (LocalStatus, error) {
	if !s.operationMu.TryLock() {
		return LocalStatus{}, ErrSyncInProgress
	}
	defer s.operationMu.Unlock()

	state, err := s.store.Load(ctx)
	if err != nil {
		return LocalStatus{}, err
	}
	if state.Status == StatusDeleted {
		return localStatus(state), nil
	}
	if state.Status == StatusDisabled {
		return localStatus(state), fmt.Errorf("ranking is not active")
	}
	if state.Status == StatusJoining {
		if _, err := s.finishJoiningLocked(ctx, state, false); err != nil {
			return localStatus(state), err
		}
		state, err = s.store.Load(ctx)
		if err != nil {
			return LocalStatus{}, err
		}
	}
	if !s.reconciled {
		state, err = s.reconcileLocked(ctx, state)
		if err != nil {
			return s.currentStatus(ctx), err
		}
		if state.Status == StatusDeleted {
			return localStatus(state), nil
		}
	}
	return s.deleteParticipantLocked(ctx, state, true)
}

// deleteParticipantLocked 在重放冲突时只允许一次 self 校准和重试，避免形成无界循环。
func (s *Service) deleteParticipantLocked(ctx context.Context, state State, retryReplay bool) (LocalStatus, error) {
	idempotencyKey, err := newIdempotencyKey("delete_", s.random)
	if err != nil {
		return localStatus(state), err
	}
	now := s.now().UTC()
	allocated, err := s.store.Update(ctx, func(next *State) error {
		if next.Status != StatusActive && next.Status != StatusPaused {
			return ErrParticipation
		}
		next.LastAllocatedSequence++
		next.LastAttemptAt = utcPointer(now)
		return nil
	})
	if err != nil {
		return localStatus(state), err
	}
	_, err = s.center.Delete(ctx, DeleteCommand{
		Credentials: credentialsFromState(allocated), Sequence: allocated.LastAllocatedSequence,
		IdempotencyKey: idempotencyKey, RequestedAt: now,
	})
	if err != nil && !errors.Is(err, ErrParticipantDeleted) {
		if errors.Is(err, ErrReplayedSequence) && retryReplay {
			s.reconciled = false
			reconciled, reconcileErr := s.reconcileLocked(ctx, allocated)
			if reconcileErr != nil {
				return s.currentStatus(ctx), reconcileErr
			}
			if reconciled.Status == StatusDeleted {
				return localStatus(reconciled), nil
			}
			return s.deleteParticipantLocked(ctx, reconciled, false)
		}
		_ = s.recordError(ctx, err)
		return s.currentStatus(ctx), err
	}
	deleted, markErr := s.markDeleted(ctx)
	if markErr != nil {
		return LocalStatus{}, markErr
	}
	return localStatus(deleted), nil
}

func (s *Service) Leaderboard(ctx context.Context, period LeaderboardPeriod, metric LeaderboardMetric) (Leaderboard, error) {
	if !validLeaderboardPeriod(period) || !validLeaderboardMetric(metric) {
		return Leaderboard{}, fmt.Errorf("invalid leaderboard selection")
	}
	now := s.now()
	if cached, ok := s.readCache.leaderboard(period, metric, now); ok {
		return cached, nil
	}
	board, err := s.center.Leaderboard(ctx, period, metric)
	if err != nil {
		return Leaderboard{}, err
	}
	s.readCache.storeLeaderboard(period, metric, board, now)
	return board, nil
}

func (s *Service) LeaderboardMetadata(ctx context.Context) (LeaderboardMetadata, error) {
	now := s.now()
	if cached, ok := s.readCache.leaderboardMetadata(now); ok {
		return cached, nil
	}
	metadata, err := s.center.LeaderboardMetadata(ctx)
	if err != nil {
		return LeaderboardMetadata{}, err
	}
	s.readCache.storeLeaderboardMetadata(metadata, now)
	return metadata, nil
}

func (s *Service) finishJoiningLocked(ctx context.Context, state State, syncAfterRegistration bool) (LocalStatus, error) {
	now := s.now().UTC()
	if _, err := s.store.Update(ctx, func(next *State) error {
		if next.Status != StatusJoining {
			return fmt.Errorf("ranking registration state changed")
		}
		next.LastAttemptAt = utcPointer(now)
		return nil
	}); err != nil {
		return localStatus(state), err
	}
	participant, err := s.center.Register(ctx, RegistrationCommand{
		Credentials: credentialsFromState(state), IdempotencyKey: state.RegistrationIdempotencyKey,
		DisplayName: state.DisplayName, AvatarID: state.AvatarID, RequestedAt: now,
	})
	if err != nil {
		if errors.Is(err, ErrParticipantDeleted) {
			deleted, markErr := s.markDeleted(ctx)
			if markErr != nil {
				return LocalStatus{}, markErr
			}
			return localStatus(deleted), ErrParticipantDeleted
		}
		_ = s.recordError(ctx, err)
		return s.currentStatus(ctx), err
	}
	if participant.ParticipantID == "" || strings.ContainsAny(participant.ParticipantID, " \t\r\n") || participant.DisplayName != state.DisplayName || participant.AvatarID != state.AvatarID || participant.CreatedAt.IsZero() || participant.Deleted {
		err := fmt.Errorf("ranking center returned an inconsistent registration")
		_ = s.recordError(ctx, err)
		return s.currentStatus(ctx), err
	}
	active, err := s.store.Update(ctx, func(next *State) error {
		if next.Status != StatusJoining || next.PublicKey != state.PublicKey || next.RegistrationIdempotencyKey != state.RegistrationIdempotencyKey {
			return fmt.Errorf("ranking registration state changed")
		}
		next.Status = StatusActive
		next.ParticipantID = participant.ParticipantID
		next.ParticipationStartedAt = utcPointer(participant.CreatedAt)
		next.LastError = ""
		return nil
	})
	if err != nil {
		return LocalStatus{}, err
	}
	s.reconciled = true
	if !syncAfterRegistration {
		return localStatus(active), nil
	}
	if syncErr := s.syncActiveLocked(ctx, active); syncErr != nil {
		if errors.Is(syncErr, ErrParticipantDeleted) {
			return s.currentStatus(ctx), syncErr
		}
		// 注册已经成功，动态同步错误通过状态字段呈现，由 runner 按周期重试。
		return s.currentStatus(ctx), nil
	}
	return s.currentStatus(ctx), nil
}

func (s *Service) reconcileLocked(ctx context.Context, state State) (State, error) {
	now := s.now().UTC()
	if _, err := s.store.Update(ctx, func(next *State) error {
		next.LastAttemptAt = utcPointer(now)
		return nil
	}); err != nil {
		return State{}, err
	}
	remote, err := s.center.Self(ctx, credentialsFromState(state), now)
	if err != nil {
		if errors.Is(err, ErrParticipantDeleted) {
			return s.markDeleted(ctx)
		}
		_ = s.recordError(ctx, err)
		return State{}, err
	}
	if remote.Status == "deleted" {
		return s.markDeleted(ctx)
	}
	if remote.Status != "active" && remote.Status != "revoked" {
		err := fmt.Errorf("ranking center returned an invalid participant status")
		_ = s.recordError(ctx, err)
		return State{}, err
	}
	if remote.ParticipantID != state.ParticipantID || remote.DisplayName != state.DisplayName || remote.AvatarID != state.AvatarID {
		err := fmt.Errorf("ranking center participant identity does not match local state")
		_ = s.recordError(ctx, err)
		return State{}, err
	}
	updated, err := s.store.Update(ctx, func(next *State) error {
		if remote.LastSequence > next.LastAllocatedSequence {
			next.LastAllocatedSequence = remote.LastSequence
		}
		next.LastError = ""
		return nil
	})
	if err != nil {
		return State{}, err
	}
	s.reconciled = true
	return updated, nil
}

func (s *Service) syncActiveLocked(ctx context.Context, state State) error {
	now := s.now().UTC()
	metadata, err := s.LeaderboardMetadata(ctx)
	if err != nil {
		_ = s.recordError(ctx, err)
		return err
	}
	periodLocation, err := time.LoadLocation(metadata.PeriodTimezone)
	if err != nil {
		err = fmt.Errorf("%w: period timezone is invalid", ErrIncompatibleCenter)
		_ = s.recordError(ctx, err)
		return err
	}
	// 榜单日界线只服从中心 metadata；聚合器再按 instant 转成 Keeper 自身的存储时区查询。
	periodNow := now.In(periodLocation)
	todayStart := time.Date(periodNow.Year(), periodNow.Month(), periodNow.Day(), 0, 0, 0, 0, periodLocation)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yesterdayKey := yesterdayStart.Format("2006-01-02")
	var previousErr error
	joinedBeforeToday := state.ParticipationStartedAt != nil && state.ParticipationStartedAt.Before(todayStart)
	if joinedBeforeToday && now.Before(todayStart.Add(2*time.Hour)) {
		yesterdayEventID, err := s.aggregator.LatestEventID(ctx, yesterdayStart, todayStart)
		if err != nil {
			_ = s.recordError(ctx, err)
			previousErr = err
		} else if yesterdayEventID == 0 && state.LastSuccessfulCompleteDay != yesterdayKey {
			updated, updateErr := s.store.Update(ctx, func(next *State) error {
				if next.Status != StatusActive {
					return ErrParticipation
				}
				next.LastSuccessfulCompleteDay = yesterdayKey
				next.LastSuccessfulCompleteEventID = 0
				return nil
			})
			if updateErr != nil {
				return updateErr
			}
			state = updated
		} else if state.LastSuccessfulCompleteDay != yesterdayKey || state.LastSuccessfulCompleteEventID != yesterdayEventID {
			if _, err := s.submitRangeLocked(ctx, yesterdayStart, todayStart, metadata.PeriodTimezone, yesterdayKey, true, yesterdayEventID, now); err != nil {
				if errors.Is(err, ErrParticipantDeleted) || errors.Is(err, ErrReplayedSequence) {
					return err
				}
				previousErr = err
			}
		}
	}

	latestEventID, err := s.aggregator.LatestEventID(ctx, todayStart, periodNow)
	if err != nil {
		_ = s.recordError(ctx, err)
		return errors.Join(previousErr, err)
	}
	todayKey := todayStart.Format("2006-01-02")
	if latestEventID == 0 {
		s.lastDynamicDay = todayKey
		s.lastDynamicEventID = 0
	} else if s.lastDynamicDay != todayKey || s.lastDynamicEventID != latestEventID {
		if _, err := s.submitRangeLocked(ctx, todayStart, periodNow, metadata.PeriodTimezone, todayKey, false, 0, now); err != nil {
			return errors.Join(previousErr, err)
		}
		s.lastDynamicDay = todayKey
		s.lastDynamicEventID = latestEventID
	}
	if previousErr != nil {
		_ = s.recordError(ctx, previousErr)
	}
	return previousErr
}

func (s *Service) submitRangeLocked(ctx context.Context, start, end time.Time, periodTimezone, dayKey string, complete bool, completeEventID int64, snapshotAt time.Time) (ReportReceipt, error) {
	metrics, err := s.aggregator.AggregateDay(ctx, start, end)
	if err != nil {
		_ = s.recordError(ctx, err)
		return ReportReceipt{}, err
	}
	if err := ValidateMetrics(metrics); err != nil {
		_ = s.recordError(ctx, err)
		return ReportReceipt{}, err
	}
	idempotencyKey, err := newIdempotencyKey("report_", s.random)
	if err != nil {
		return ReportReceipt{}, err
	}
	allocated, err := s.store.Update(ctx, func(next *State) error {
		if next.Status != StatusActive {
			return fmt.Errorf("ranking participant is not active")
		}
		next.LastAllocatedSequence++
		next.LastAttemptAt = utcPointer(snapshotAt)
		return nil
	})
	if err != nil {
		return ReportReceipt{}, err
	}
	receipt, err := s.center.SubmitReport(ctx, ReportCommand{
		Credentials: credentialsFromState(allocated), Sequence: allocated.LastAllocatedSequence,
		IdempotencyKey: idempotencyKey, SnapshotAt: snapshotAt, PeriodTimezone: periodTimezone, DayKey: dayKey, Complete: complete, Metrics: metrics,
	})
	if err != nil {
		if errors.Is(err, ErrParticipantDeleted) {
			if _, markErr := s.markDeleted(ctx); markErr != nil {
				return ReportReceipt{}, markErr
			}
			return ReportReceipt{}, ErrParticipantDeleted
		}
		if errors.Is(err, ErrReplayedSequence) {
			s.reconciled = false
		}
		_ = s.recordError(ctx, err)
		return ReportReceipt{}, err
	}
	syncedAt := receipt.SyncedAt.UTC()
	if syncedAt.IsZero() {
		syncedAt = snapshotAt
	}
	_, err = s.store.Update(ctx, func(next *State) error {
		next.LastSuccessfulSyncAt = utcPointer(syncedAt)
		if complete {
			next.LastSuccessfulCompleteDay = dayKey
			next.LastSuccessfulCompleteEventID = completeEventID
		}
		next.LastError = ""
		return nil
	})
	if err != nil {
		return ReportReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) markDeleted(context.Context) (State, error) {
	// 中心已经确认删除后，本地墓碑不能被浏览器断开取消；独立上限防止持久化无限等待。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.store.Update(ctx, func(next *State) error {
		next.Status = StatusDeleted
		next.LastError = ""
		return nil
	})
}

func (s *Service) recordError(ctx context.Context, cause error) error {
	if cause == nil {
		return nil
	}
	message := cause.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	_, err := s.store.Update(persistenceContext(ctx), func(next *State) error {
		next.LastError = message
		return nil
	})
	return err
}

func (s *Service) currentStatus(ctx context.Context) LocalStatus {
	state, err := s.store.Load(persistenceContext(ctx))
	if err != nil {
		return LocalStatus{}
	}
	return localStatus(state)
}

func localStatus(state State) LocalStatus {
	return LocalStatus{
		Status: state.Status, DisplayName: state.DisplayName, AvatarID: state.AvatarID,
		ParticipantID:             state.ParticipantID,
		LastSuccessfulCompleteDay: state.LastSuccessfulCompleteDay,
		LastSuccessfulSyncAt:      state.LastSuccessfulSyncAt, LastAttemptAt: state.LastAttemptAt, LastError: state.LastError,
	}
}

func credentialsFromState(state State) Credentials {
	return Credentials{PublicKey: state.PublicKey, PrivateKey: state.PrivateKey, ParticipantID: state.ParticipantID}
}

func utcPointer(value time.Time) *time.Time {
	utc := value.UTC()
	return &utc
}

func persistenceContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	// 保持调用方 deadline，避免错误状态回写突破 runner 的 10 秒硬超时。
	return ctx
}

func normalizeProfile(displayName string, avatarID uint8) (string, error) {
	if !utf8.ValidString(displayName) {
		return "", ErrInvalidProfile
	}
	name := norm.NFKC.String(displayName)
	for _, character := range name {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf, unicode.Zl, unicode.Zp) {
			return "", ErrInvalidProfile
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxDisplayNameRunes || len(name) > 128 || avatarID < MinAvatarID || avatarID > MaxAvatarID {
		return "", ErrInvalidProfile
	}
	hasLetterOrDigit := false
	for _, character := range name {
		switch {
		case unicode.IsLetter(character), unicode.IsDigit(character):
			hasLetterOrDigit = true
		case character == '-', character == '_':
		default:
			return "", ErrInvalidProfile
		}
	}
	if !hasLetterOrDigit || containsPhonePattern(name) {
		return "", ErrInvalidProfile
	}
	return name, nil
}

func containsPhonePattern(name string) bool {
	digits := 0
	for _, character := range name {
		switch {
		case unicode.IsDigit(character):
			digits++
			if digits >= 10 {
				return true
			}
		case digits > 0 && (character == '-' || character == '_'):
			continue
		default:
			digits = 0
		}
	}
	return false
}
