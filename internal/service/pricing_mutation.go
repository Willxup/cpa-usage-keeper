package service

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

// mutatePricing 串行完成写事务、事务内候选编译和提交后的原子发布。
func (s *pricingService) mutatePricing(ctx context.Context, callback func(*gorm.DB) error) (*pricing.Snapshot, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	var candidate *pricing.Snapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := callback(tx); err != nil {
			return err
		}
		var err error
		candidate, err = repository.LoadPricingSnapshot(ctx, tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	// Transaction 返回 nil 已代表 COMMIT 成功；发布阶段只执行不可失败的原子指针替换。
	s.catalog.Replace(candidate)
	return candidate, nil
}
