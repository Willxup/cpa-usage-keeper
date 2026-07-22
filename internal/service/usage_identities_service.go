package service

import (
	"context"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type ListUsageIdentitiesRequest struct {
	AuthType   *entities.UsageIdentityAuthType
	ActiveOnly *bool
	Types      []string
	Sort       string
	Page       int
	PageSize   int
}

type UsageIdentityTypeCount = repodto.UsageIdentityTypeCount

type UsageCredentialHealthBucket struct {
	StartTime time.Time
	EndTime   time.Time
	Success   int64
	Failure   int64
	Rate      float64
}

type UsageCredentialHealthSnapshot struct {
	WindowSeconds int64
	BucketSeconds int64
	WindowStart   time.Time
	WindowEnd     time.Time
	TotalSuccess  int64
	TotalFailure  int64
	SuccessRate   float64
	Buckets       []UsageCredentialHealthBucket
}

type ListUsageIdentitiesResponse struct {
	Items            []entities.UsageIdentity
	Total            int64
	TypeCounts       []UsageIdentityTypeCount
	CredentialHealth []UsageCredentialHealthSnapshot
}

type UsageIdentityProvider interface {
	ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error)
	ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error)
	ListActiveUsageIdentitiesPage(context.Context, ListUsageIdentitiesRequest) (ListUsageIdentitiesResponse, error)
	UpdateUsageIdentityAlias(context.Context, int64, string) (entities.UsageIdentity, error)
}

type UsageIdentityServiceOptions struct {
	OnDisplayNameChanged func(entities.UsageIdentity)
}

type usageIdentityService struct {
	db                   *gorm.DB
	recentUsage          *repository.UsageRecentEventCache
	now                  func() time.Time
	onDisplayNameChanged func(entities.UsageIdentity)
}

func NewUsageIdentityService(db *gorm.DB) UsageIdentityProvider {
	return NewUsageIdentityServiceWithRecentCache(db, nil)
}

func NewUsageIdentityServiceWithRecentCache(db *gorm.DB, recentUsage *repository.UsageRecentEventCache) UsageIdentityProvider {
	return NewUsageIdentityServiceWithOptions(db, recentUsage, UsageIdentityServiceOptions{})
}

func NewUsageIdentityServiceWithOptions(db *gorm.DB, recentUsage *repository.UsageRecentEventCache, options UsageIdentityServiceOptions) UsageIdentityProvider {
	return &usageIdentityService{db: db, recentUsage: recentUsage, now: time.Now, onDisplayNameChanged: options.OnDisplayNameChanged}
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
	items, total, typeCounts, err := repository.ListActiveUsageIdentitiesPage(ctx, s.db, repository.ListUsageIdentitiesPageRequest{
		AuthType:   request.AuthType,
		ActiveOnly: request.ActiveOnly,
		Types:      request.Types,
		Sort:       request.Sort,
		Page:       request.Page,
		PageSize:   request.PageSize,
	})
	if err != nil {
		return ListUsageIdentitiesResponse{}, err
	}
	return ListUsageIdentitiesResponse{Items: items, Total: total, TypeCounts: typeCounts, CredentialHealth: s.credentialHealthSnapshots(items)}, nil
}

func (s *usageIdentityService) UpdateUsageIdentityAlias(ctx context.Context, id int64, alias string) (entities.UsageIdentity, error) {
	if id <= 0 {
		return entities.UsageIdentity{}, ErrInvalidID
	}
	// UPDATE 由 dbresolver 自动路由 writer；结果回读再用官方 Write clause 固定到同一物理池。
	if err := repository.UpdateUsageIdentityAlias(ctx, s.db, id, alias); err != nil {
		return entities.UsageIdentity{}, err
	}
	updated, err := repository.FindUsageIdentityByID(ctx, s.db.Clauses(dbresolver.Write), id)
	if err != nil {
		return entities.UsageIdentity{}, err
	}
	if s.onDisplayNameChanged != nil {
		s.onDisplayNameChanged(updated)
	}
	return updated, nil
}

func (s *usageIdentityService) credentialHealthSnapshots(items []entities.UsageIdentity) []UsageCredentialHealthSnapshot {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	snapshots := make([]UsageCredentialHealthSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, mapUsageCredentialHealthSnapshot(s.credentialHealthSnapshot(item, now)))
	}
	return snapshots
}

func (s *usageIdentityService) credentialHealthSnapshot(item entities.UsageIdentity, now time.Time) repository.CredentialHealthSnapshot {
	authType, ok := usageIdentityEventAuthType(item.AuthType)
	if !ok || s.recentUsage == nil {
		return repository.EmptyCredentialHealthSnapshot(now)
	}
	snapshot, ok := s.recentUsage.CredentialHealth(authType, item.Identity, now)
	if !ok {
		return repository.EmptyCredentialHealthSnapshot(now)
	}
	return snapshot
}

func usageIdentityEventAuthType(authType entities.UsageIdentityAuthType) (string, bool) {
	switch authType {
	case entities.UsageIdentityAuthTypeAuthFile:
		return "oauth", true
	case entities.UsageIdentityAuthTypeAIProvider:
		return "apikey", true
	default:
		return "", false
	}
}

func mapUsageCredentialHealthSnapshot(snapshot repository.CredentialHealthSnapshot) UsageCredentialHealthSnapshot {
	buckets := make([]UsageCredentialHealthBucket, 0, len(snapshot.Buckets))
	for _, bucket := range snapshot.Buckets {
		buckets = append(buckets, UsageCredentialHealthBucket{
			StartTime: bucket.StartTime,
			EndTime:   bucket.EndTime,
			Success:   bucket.Success,
			Failure:   bucket.Failure,
			Rate:      bucket.Rate,
		})
	}
	return UsageCredentialHealthSnapshot{
		WindowSeconds: snapshot.WindowSeconds,
		BucketSeconds: snapshot.BucketSeconds,
		WindowStart:   snapshot.WindowStart,
		WindowEnd:     snapshot.WindowEnd,
		TotalSuccess:  snapshot.TotalSuccess,
		TotalFailure:  snapshot.TotalFailure,
		SuccessRate:   snapshot.SuccessRate,
		Buckets:       buckets,
	}
}
