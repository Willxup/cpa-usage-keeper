package service

import (
	"context"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type ListUsageIdentitiesRequest struct {
	AuthType *entities.UsageIdentityAuthType
	Page     int
	PageSize int
}

type ListUsageIdentitiesResponse struct {
	Items []entities.UsageIdentity
	Total int64
}

type UsageIdentityProvider interface {
	ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error)
	ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error)
	ListActiveUsageIdentitiesPage(context.Context, ListUsageIdentitiesRequest) (ListUsageIdentitiesResponse, error)
}

type usageIdentityService struct {
	db *gorm.DB
}

func NewUsageIdentityService(db *gorm.DB) UsageIdentityProvider {
	return &usageIdentityService{db: db}
}

func (s *usageIdentityService) ListUsageIdentities(ctx context.Context) ([]entities.UsageIdentity, error) {
	// identities 页面需要全量历史，包含已删除身份，用于展示 deleted 状态和统计数据。
	return repository.ListUsageIdentities(ctx, s.db)
}

func (s *usageIdentityService) ListActiveUsageIdentities(ctx context.Context) ([]entities.UsageIdentity, error) {
	// source 解析和筛选只需要活跃身份，过滤条件下推到 repository 的 SQL 查询中执行。
	return repository.ListActiveUsageIdentities(ctx, s.db)
}

func (s *usageIdentityService) ListActiveUsageIdentitiesPage(ctx context.Context, request ListUsageIdentitiesRequest) (ListUsageIdentitiesResponse, error) {
	items, total, err := repository.ListActiveUsageIdentitiesPage(ctx, s.db, repository.ListUsageIdentitiesPageRequest{
		AuthType: request.AuthType,
		Page:     request.Page,
		PageSize: request.PageSize,
	})
	if err != nil {
		return ListUsageIdentitiesResponse{}, err
	}
	return ListUsageIdentitiesResponse{Items: items, Total: total}, nil
}
