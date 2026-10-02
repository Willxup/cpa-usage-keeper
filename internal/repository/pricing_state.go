package repository

import (
	"errors"
	"fmt"
	"math"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// LoadPricingConfigRevision 读取已提交的配置修订；首次保存前无单例行等同于修订 0。
// 列表读取只查状态，不因 GET 创建控制行。
func LoadPricingConfigRevision(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	var state entities.PricingState
	// 从写库读取修订，确保它与服务锁内取得的已发布快照对应同一提交序列。
	err := db.Clauses(dbresolver.Write).Where("id = ?", 1).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load pricing config revision: %w", err)
	}
	return state.ConfigRevision, nil
}

// AdvancePricingConfigRevision 在调用方配置事务内递增一次；提交失败会随配置一起回滚。
func AdvancePricingConfigRevision(tx *gorm.DB) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("database is nil")
	}
	// 写命令先从 writer 取旧值；同一服务的 mutationMu 已串行化所有价格配置事务。
	var state entities.PricingState
	err := tx.Clauses(dbresolver.Write).Where("id = ?", 1).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state = entities.PricingState{ID: 1, ConfigRevision: 1}
		if err := tx.Clauses(dbresolver.Write).Create(&state).Error; err != nil {
			return 0, fmt.Errorf("initialize pricing config revision: %w", err)
		}
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load pricing config revision for update: %w", err)
	}
	if state.ConfigRevision == math.MaxInt64 {
		return 0, fmt.Errorf("pricing config revision exhausted")
	}
	next := state.ConfigRevision + 1
	result := tx.Clauses(dbresolver.Write).Model(&entities.PricingState{}).
		Where("id = ? AND config_revision = ?", 1, state.ConfigRevision).
		Update("config_revision", next)
	if result.Error != nil {
		return 0, fmt.Errorf("advance pricing config revision: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, fmt.Errorf("pricing config revision changed concurrently")
	}
	return next, nil
}
