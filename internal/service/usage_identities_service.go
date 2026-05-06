package service

import (
	"context"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type UsageIdentityProvider interface {
	ListUsageIdentities(context.Context) ([]models.UsageIdentity, error)
	ListActiveUsageIdentities(context.Context) ([]models.UsageIdentity, error)
}

type usageIdentityService struct {
	db *gorm.DB
}

func NewUsageIdentityService(db *gorm.DB) UsageIdentityProvider {
	return &usageIdentityService{db: db}
}

func (s *usageIdentityService) ListUsageIdentities(ctx context.Context) ([]models.UsageIdentity, error) {
	// identities 页面需要全量历史，包含已删除身份，用于展示 deleted 状态和统计数据。
	return repository.ListUsageIdentities(ctx, s.db)
}

func (s *usageIdentityService) ListActiveUsageIdentities(ctx context.Context) ([]models.UsageIdentity, error) {
	// source 解析和筛选只需要活跃身份，过滤条件下推到 repository 的 SQL 查询中执行。
	return repository.ListActiveUsageIdentities(ctx, s.db)
}
