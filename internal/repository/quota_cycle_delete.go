package repository

import (
	"context"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// CodexQuotaCycleDeletion 提供本次删除清理队列和当前状态所需的周期归属，清理后即释放。
type CodexQuotaCycleDeletion struct {
	entities.QuotaCycle
	// cycles 按最新观察倒序保存删除前的父行，用于区分校准后时间相邻的周期。
	cycles []entities.QuotaCycle
}

// MatchesObservation 复用写入的最新行优先、其余行按 reset 最近匹配规则。
func (deletion CodexQuotaCycleDeletion) MatchesObservation(observation repositorydto.CodexMainQuotaObservation) bool {
	provider := strings.ToLower(strings.TrimSpace(observation.Provider))
	if provider == "" {
		provider = "codex"
	}
	quotaKey, validRole := repositorydto.QuotaWindowKey(provider, observation.WindowRole)
	if !validRole || provider != deletion.Provider || strings.TrimSpace(observation.AuthIndex) != deletion.AuthIndex || quotaKey != deletion.QuotaKey ||
		!quotaCycleMatchesObservation(deletion.QuotaCycle, observation) {
		return false
	}
	if len(deletion.cycles) > 0 && quotaCycleMatchesObservation(deletion.cycles[0], observation) {
		return deletion.cycles[0].ID == deletion.ID
	}
	matched, found := closestMatchingQuotaCycle(deletion.cycles, observation.WindowSeconds, observation.ResetAt)
	return found && matched.ID == deletion.ID
}

// DeleteCodexQuotaCycle 在同一写事务内核对归属并删除百分比段和父周期。
func DeleteCodexQuotaCycle(ctx context.Context, db *gorm.DB, authIndex string, cycleID int64) (CodexQuotaCycleDeletion, error) {
	return DeleteQuotaCycle(ctx, db, "codex", authIndex, cycleID)
}

func DeleteQuotaCycle(ctx context.Context, db *gorm.DB, provider string, authIndex string, cycleID int64) (CodexQuotaCycleDeletion, error) {
	var deletion CodexQuotaCycleDeletion
	cycle := &deletion.QuotaCycle
	authIndex = strings.TrimSpace(authIndex)
	provider = strings.ToLower(strings.TrimSpace(provider))
	primaryKey, validProvider := repositorydto.QuotaWindowKey(provider, "primary")
	secondaryKey, _ := repositorydto.QuotaWindowKey(provider, "secondary")
	if db == nil || authIndex == "" || cycleID <= 0 || !validProvider {
		return deletion, fmt.Errorf("delete codex quota cycle: invalid database, auth_index or cycle_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := db.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND provider = ? AND auth_index = ? AND quota_key IN ?",
			cycleID, provider, authIndex, []string{primaryKey, secondaryKey}).Take(cycle).Error; err != nil {
			return err
		}
		// 只在低频删除时读取该账号角色的父行身份，不给正常采样增加查询或改变写入接口。
		if err := tx.Select("id", "window_seconds", "reset_at", "last_observed_at").
			Where("provider = ? AND auth_index = ? AND quota_key = ?", provider, authIndex, cycle.QuotaKey).
			Order("last_observed_at DESC, id DESC").Find(&deletion.cycles).Error; err != nil {
			return err
		}
		if err := tx.Where("cycle_id = ?", cycle.ID).Delete(&entities.QuotaPercentSegment{}).Error; err != nil {
			return err
		}
		return tx.Delete(cycle).Error
	})
	return deletion, err
}
