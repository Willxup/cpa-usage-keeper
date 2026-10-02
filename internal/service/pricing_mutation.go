package service

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

// mutatePricingRevision 串行完成完整价格事务、修订递增和提交后发布。
// 该锁只保护配置/修订的一致读取，不等待已经取得旧 Resolver 的事件批次。
func (s *pricingService) mutatePricingRevision(ctx context.Context, callback func(*gorm.DB) error) (*pricing.Snapshot, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.recalculationRunning {
		return nil, 0, ErrPricingBusy
	}

	var candidate *pricing.Snapshot
	var revision int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := callback(tx); err != nil {
			return err
		}
		var err error
		candidate, err = repository.LoadPricingSnapshot(ctx, tx)
		if err != nil {
			return err
		}
		revision, err = repository.AdvancePricingConfigRevision(tx)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	// Transaction 返回 nil 已代表 COMMIT 成功；发布阶段只执行不可失败的原子指针替换。
	s.catalog.Replace(candidate)
	return candidate, revision, nil
}
