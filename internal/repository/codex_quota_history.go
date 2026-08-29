package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	codexQuotaHistoryWriteBatchSize = 32
	// 所有上游重置时间统一使用固定两分钟容差，避免直接时刻的秒级漂移拆分周期。
	codexQuotaResetTolerance = 120 * time.Second
	codexQuotaProvider       = "codex"
	codexPrimaryQuotaKey     = "rate_limit.primary_window"
	codexSecondaryQuotaKey   = "rate_limit.secondary_window"
)

// WriteCodexMainQuotaObservations 把 Codex observation 规范化后写入通用额度历史父子表。
func WriteCodexMainQuotaObservations(ctx context.Context, db *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
	if db == nil {
		return fmt.Errorf("write codex quota history: database is nil")
	}
	if len(observations) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalized := make([]repositorydto.CodexMainQuotaObservation, len(observations))
	for index, observation := range observations {
		validated, err := normalizeCodexMainQuotaObservation(observation)
		if err != nil {
			return fmt.Errorf("validate codex quota observation %d: %w", index, err)
		}
		normalized[index] = validated
	}

	for start := 0; start < len(normalized); start += codexQuotaHistoryWriteBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("write codex quota history: %w", err)
		}
		end := min(start+codexQuotaHistoryWriteBatchSize, len(normalized))
		batch := normalized[start:end]
		// 周期定位、稳定边界升级、尾段比较和父子写入必须共享同一个 writer 事务。
		err := db.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
			for _, observation := range batch {
				if err := applyCodexMainQuotaObservation(tx, observation); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("write codex quota history batch %d-%d: %w", start, end, err)
		}
	}
	return nil
}

// LoadLatestCodexQuotaHistoryState 从通用表恢复一个 Codex 主窗口的最新周期和尾段。
func LoadLatestCodexQuotaHistoryState(ctx context.Context, db *gorm.DB, authIndex string, windowRole string) (repositorydto.CodexQuotaHistoryState, error) {
	state := repositorydto.CodexQuotaHistoryState{}
	if db == nil {
		return state, fmt.Errorf("load codex quota history state: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return state, fmt.Errorf("load codex quota history state: auth_index is required")
	}
	windowRole = strings.ToLower(strings.TrimSpace(windowRole))
	quotaKey, ok := codexQuotaKey(windowRole)
	if !ok {
		return state, fmt.Errorf("load codex quota history state: invalid window role %q", windowRole)
	}

	var cycle entities.QuotaCycle
	err := db.WithContext(ctx).Clauses(dbresolver.Write).
		Where("provider = ? AND auth_index = ? AND quota_key = ?", codexQuotaProvider, authIndex, quotaKey).
		Order("last_observed_at DESC, id DESC").
		Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("load latest quota cycle: %w", err)
	}

	state = repositorydto.CodexQuotaHistoryState{
		Found:         true,
		WindowSeconds: cycle.WindowSeconds,
		ResetAtSource: string(cycle.ResetAtSource),
		ResetAt:       cycle.ResetAt,
	}
	var tail entities.QuotaPercentSegment
	err = db.WithContext(ctx).Clauses(dbresolver.Write).
		Where("cycle_id = ?", cycle.ID).
		Order("first_observed_at DESC, id DESC").
		Take(&tail).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, nil
	}
	if err != nil {
		return repositorydto.CodexQuotaHistoryState{}, fmt.Errorf("load latest quota percent segment: %w", err)
	}
	state.HasTail = true
	state.TailRemainingPercent = tail.RemainingPercent
	state.TailLastObservedAt = tail.LastObservedAt
	return state, nil
}

func normalizeCodexMainQuotaObservation(observation repositorydto.CodexMainQuotaObservation) (repositorydto.CodexMainQuotaObservation, error) {
	observation.AuthIndex = strings.TrimSpace(observation.AuthIndex)
	if observation.AuthIndex == "" {
		return observation, fmt.Errorf("auth_index is required")
	}
	observation.WindowRole = strings.ToLower(strings.TrimSpace(observation.WindowRole))
	if _, ok := codexQuotaKey(observation.WindowRole); !ok {
		return observation, fmt.Errorf("invalid window role %q", observation.WindowRole)
	}
	if observation.WindowSeconds <= 0 {
		return observation, fmt.Errorf("window seconds must be positive")
	}
	if observation.WindowSeconds > math.MaxInt64/int64(time.Second) {
		return observation, fmt.Errorf("window seconds exceed time duration range")
	}
	observation.ResetAtSource = strings.ToLower(strings.TrimSpace(observation.ResetAtSource))
	if observation.ResetAtSource != string(entities.QuotaResetAtSourceAbsolute) && observation.ResetAtSource != string(entities.QuotaResetAtSourceRelative) {
		return observation, fmt.Errorf("invalid reset source %q", observation.ResetAtSource)
	}
	if observation.ResetAt.IsZero() {
		return observation, fmt.Errorf("reset_at is required")
	}
	if observation.RemainingPercent < 0 || observation.RemainingPercent > 100 {
		return observation, fmt.Errorf("remaining percent must be between 0 and 100")
	}
	if observation.ObservationCount < 1 {
		return observation, fmt.Errorf("observation count must be positive")
	}
	if observation.FirstObservedAt.IsZero() || observation.LastObservedAt.IsZero() {
		return observation, fmt.Errorf("observation time is required")
	}
	if observation.FirstObservedAt.After(observation.LastObservedAt) {
		return observation, fmt.Errorf("first observed time is after last observed time")
	}
	observation.ResetAt = timeutil.NormalizeStorageTime(observation.ResetAt)
	observation.FirstObservedAt = timeutil.NormalizeStorageTime(observation.FirstObservedAt)
	observation.LastObservedAt = timeutil.NormalizeStorageTime(observation.LastObservedAt)
	return observation, nil
}

func applyCodexMainQuotaObservation(tx *gorm.DB, observation repositorydto.CodexMainQuotaObservation) error {
	quotaKey, _ := codexQuotaKey(observation.WindowRole)
	cycle, found, err := loadCurrentQuotaCycle(tx, observation.AuthIndex, quotaKey)
	if err != nil {
		return err
	}
	if found && cycle.WindowSeconds != observation.WindowSeconds {
		// 窗口变化优先于 reset；只有观察时间更旧时才把它视为切换前的迟到事实。
		if observation.LastObservedAt.Before(cycle.LastObservedAt) {
			return nil
		}
		found = false
	} else if found && !quotaResetTimesMatch(cycle.ResetAt, observation.ResetAt) {
		// 相同窗口仍按观察时间与 reset 顺序拒绝旧事实，超过两分钟的未来 reset 建立新周期。
		if observation.LastObservedAt.Before(cycle.LastObservedAt) || observation.ResetAt.Before(cycle.ResetAt) {
			return nil
		}
		found = false
	}

	created := false
	if !found {
		cycle = newQuotaCycle(observation, quotaKey)
		if err := tx.Create(&cycle).Error; err != nil {
			return fmt.Errorf("create quota cycle: %w", err)
		}
		created = true
	} else {
		// 旧 observation 不能只凭可信来源身份反向校准已经推进的当前周期。
		if observation.LastObservedAt.Before(cycle.LastObservedAt) {
			return nil
		}
		if err := upgradeQuotaCycleBoundary(tx, &cycle, observation); err != nil {
			return err
		}
	}

	var tail entities.QuotaPercentSegment
	err = tx.Where("cycle_id = ?", cycle.ID).
		Order("first_observed_at DESC, id DESC").
		Take(&tail).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		segment := newQuotaPercentSegment(cycle.ID, observation)
		if err := tx.Create(&segment).Error; err != nil {
			return fmt.Errorf("create first quota percent segment: %w", err)
		}
		if !created {
			return updateQuotaCycleObservedTimes(tx, cycle, observation)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load quota percent tail: %w", err)
	}

	if observation.LastObservedAt.Before(tail.LastObservedAt) {
		return nil
	}
	// 专门额度接口可信地确认同周期百分比更高时，较低尾段已不可能真实存在，必须在本事务内纠正数据库事实。
	if observation.Authoritative && observation.RemainingPercent > tail.RemainingPercent {
		return correctQuotaPercentTail(tx, cycle, observation)
	}
	if observation.LastObservedAt.Equal(tail.LastObservedAt) {
		if observation.RemainingPercent != tail.RemainingPercent {
			logrus.WithFields(logrus.Fields{
				"auth_index":       observation.AuthIndex,
				"quota_key":        quotaKey,
				"cycle_id":         cycle.ID,
				"last_observed_at": observation.LastObservedAt,
			}).Warn("quota history observation conflicts at the same timestamp")
		}
		return nil
	}
	if !observation.FirstObservedAt.After(tail.LastObservedAt) {
		logrus.WithFields(logrus.Fields{
			"auth_index": observation.AuthIndex,
			"quota_key":  quotaKey,
			"cycle_id":   cycle.ID,
		}).Warn("quota history overlapping observation ignored")
		return nil
	}
	if observation.RemainingPercent > tail.RemainingPercent {
		return nil
	}

	if observation.RemainingPercent == tail.RemainingPercent {
		updates := map[string]any{
			"last_observed_at":  timeutil.FormatSortableStorageTime(observation.LastObservedAt),
			"observation_count": gorm.Expr("observation_count + ?", observation.ObservationCount),
			"updated_at":        timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now())),
		}
		if err := tx.Model(&entities.QuotaPercentSegment{}).Where("id = ?", tail.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update quota percent segment: %w", err)
		}
	} else {
		segment := newQuotaPercentSegment(cycle.ID, observation)
		if err := tx.Create(&segment).Error; err != nil {
			return fmt.Errorf("create quota percent segment: %w", err)
		}
	}
	return updateQuotaCycleObservedTimes(tx, cycle, observation)
}

func correctQuotaPercentTail(tx *gorm.DB, cycle entities.QuotaCycle, observation repositorydto.CodexMainQuotaObservation) error {
	// 单调不升不变量证明所有低于可信值的段都是错误后缀；一次集合删除可覆盖连续多个错误 Header 值。
	deleted := tx.Where("cycle_id = ? AND remaining_percent < ?", cycle.ID, observation.RemainingPercent).
		Delete(&entities.QuotaPercentSegment{})
	if deleted.Error != nil {
		return fmt.Errorf("delete corrected quota percent tail: %w", deleted.Error)
	}

	// 周期内百分比唯一键意味着可信值可能早已出现；存在时延长该段，不存在时建立新的当前基线。
	var target entities.QuotaPercentSegment
	err := tx.Where("cycle_id = ? AND remaining_percent = ?", cycle.ID, observation.RemainingPercent).Take(&target).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		target = newQuotaPercentSegment(cycle.ID, observation)
		if err := tx.Create(&target).Error; err != nil {
			return fmt.Errorf("create corrected quota percent segment: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("load corrected quota percent segment: %w", err)
	} else {
		updates := map[string]any{
			"last_observed_at":  timeutil.FormatSortableStorageTime(observation.LastObservedAt),
			"observation_count": gorm.Expr("observation_count + ?", observation.ObservationCount),
			"updated_at":        timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now())),
		}
		if err := tx.Model(&entities.QuotaPercentSegment{}).Where("id = ?", target.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update corrected quota percent segment: %w", err)
		}
	}

	logrus.WithFields(logrus.Fields{
		"auth_index":            observation.AuthIndex,
		"cycle_id":              cycle.ID,
		"remaining_percent":     observation.RemainingPercent,
		"removed_segment_count": deleted.RowsAffected,
	}).Debug("quota history percent tail corrected by authoritative observation")
	return updateQuotaCycleObservedTimes(tx, cycle, observation)
}

func loadCurrentQuotaCycle(tx *gorm.DB, authIndex string, quotaKey string) (entities.QuotaCycle, bool, error) {
	var cycle entities.QuotaCycle
	err := tx.Where("provider = ? AND auth_index = ? AND quota_key = ?", codexQuotaProvider, authIndex, quotaKey).
		Order("last_observed_at DESC, id DESC").
		Take(&cycle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cycle, false, nil
	}
	if err != nil {
		return cycle, false, fmt.Errorf("load current quota cycle: %w", err)
	}
	return cycle, true, nil
}

func quotaResetTimesMatch(left time.Time, right time.Time) bool {
	distance := left.Sub(right)
	if distance < 0 {
		distance = -distance
	}
	return distance <= codexQuotaResetTolerance
}

func newQuotaCycle(observation repositorydto.CodexMainQuotaObservation, quotaKey string) entities.QuotaCycle {
	now := timeutil.NormalizeStorageTime(time.Now())
	return entities.QuotaCycle{
		Provider:        codexQuotaProvider,
		AuthIndex:       observation.AuthIndex,
		QuotaKey:        quotaKey,
		WindowSeconds:   observation.WindowSeconds,
		ResetAtSource:   entities.QuotaResetAtSource(observation.ResetAtSource),
		WindowStartedAt: observation.ResetAt.Add(-time.Duration(observation.WindowSeconds) * time.Second),
		ResetAt:         observation.ResetAt,
		FirstObservedAt: observation.FirstObservedAt,
		LastObservedAt:  observation.LastObservedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func newQuotaPercentSegment(cycleID int64, observation repositorydto.CodexMainQuotaObservation) entities.QuotaPercentSegment {
	now := timeutil.NormalizeStorageTime(time.Now())
	return entities.QuotaPercentSegment{
		CycleID:          cycleID,
		RemainingPercent: observation.RemainingPercent,
		FirstObservedAt:  observation.FirstObservedAt,
		LastObservedAt:   observation.LastObservedAt,
		ObservationCount: observation.ObservationCount,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func upgradeQuotaCycleBoundary(tx *gorm.DB, cycle *entities.QuotaCycle, observation repositorydto.CodexMainQuotaObservation) error {
	// 可信来源可替换任意 Header 边界；普通 Header 只允许把 relative 首次升级为 absolute。
	trustedCalibration := observation.Authoritative &&
		(cycle.ResetAtSource != entities.QuotaResetAtSource(observation.ResetAtSource) || !cycle.ResetAt.Equal(observation.ResetAt))
	directUpgrade := cycle.ResetAtSource == entities.QuotaResetAtSourceRelative && observation.ResetAtSource == string(entities.QuotaResetAtSourceAbsolute)
	if !trustedCalibration && !directUpgrade {
		return nil
	}
	updates := map[string]any{
		"reset_at_source":   observation.ResetAtSource,
		"reset_at":          timeutil.FormatSortableStorageTime(observation.ResetAt),
		"window_started_at": timeutil.FormatSortableStorageTime(observation.ResetAt.Add(-time.Duration(observation.WindowSeconds) * time.Second)),
		"updated_at":        timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now())),
	}
	if err := tx.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("upgrade quota cycle boundary: %w", err)
	}
	cycle.ResetAtSource = entities.QuotaResetAtSource(observation.ResetAtSource)
	cycle.ResetAt = observation.ResetAt
	cycle.WindowStartedAt = observation.ResetAt.Add(-time.Duration(observation.WindowSeconds) * time.Second)
	return nil
}

func updateQuotaCycleObservedTimes(tx *gorm.DB, cycle entities.QuotaCycle, observation repositorydto.CodexMainQuotaObservation) error {
	firstObservedAt := cycle.FirstObservedAt
	if firstObservedAt.IsZero() || observation.FirstObservedAt.Before(firstObservedAt) {
		firstObservedAt = observation.FirstObservedAt
	}
	updates := map[string]any{
		"first_observed_at": timeutil.FormatSortableStorageTime(firstObservedAt),
		"last_observed_at":  timeutil.FormatSortableStorageTime(observation.LastObservedAt),
		"updated_at":        timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(time.Now())),
	}
	if err := tx.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update quota cycle observed times: %w", err)
	}
	return nil
}

func codexQuotaKey(role string) (string, bool) {
	switch role {
	case string(entities.CodexQuotaWindowRolePrimary):
		return codexPrimaryQuotaKey, true
	case string(entities.CodexQuotaWindowRoleSecondary):
		return codexSecondaryQuotaKey, true
	default:
		return "", false
	}
}
