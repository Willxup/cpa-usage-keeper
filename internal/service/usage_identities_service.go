package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/accessscope"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

var ErrViewerScopeReadOnly = errors.New("api key viewer scope is read only")

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
	if _, ok := accessscope.ViewerScopeFromContext(ctx); ok {
		return s.listScopedAuthFileUsageIdentities(ctx)
	}
	// identities 页面需要全量历史，包含已删除身份，用于展示 deleted 状态和统计数据。
	return repository.ListUsageIdentities(ctx, s.db)
}

func (s *usageIdentityService) ListActiveUsageIdentities(ctx context.Context) ([]entities.UsageIdentity, error) {
	if _, ok := accessscope.ViewerScopeFromContext(ctx); ok {
		return s.listScopedAuthFileUsageIdentities(ctx)
	}
	// source 解析和筛选只需要活跃身份，过滤条件下推到 repository 的 SQL 查询中执行。
	return repository.ListActiveUsageIdentities(ctx, s.db)
}

func (s *usageIdentityService) ListActiveUsageIdentitiesPage(ctx context.Context, request ListUsageIdentitiesRequest) (ListUsageIdentitiesResponse, error) {
	if _, ok := accessscope.ViewerScopeFromContext(ctx); ok {
		return s.listScopedAuthFileUsageIdentitiesPage(ctx, request)
	}
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
	if _, ok := accessscope.ViewerScopeFromContext(ctx); ok {
		return entities.UsageIdentity{}, ErrViewerScopeReadOnly
	}
	if id <= 0 {
		return entities.UsageIdentity{}, ErrInvalidID
	}
	if err := repository.UpdateUsageIdentityAlias(ctx, s.db, id, alias); err != nil {
		return entities.UsageIdentity{}, err
	}
	updated, err := repository.FindUsageIdentityByID(ctx, s.db, id)
	if err != nil {
		return entities.UsageIdentity{}, err
	}
	if s.onDisplayNameChanged != nil {
		s.onDisplayNameChanged(updated)
	}
	return updated, nil
}

func (s *usageIdentityService) listScopedAuthFileUsageIdentities(ctx context.Context) ([]entities.UsageIdentity, error) {
	scope, ok := accessscope.ViewerScopeFromContext(ctx)
	if !ok {
		return []entities.UsageIdentity{}, nil
	}
	authIndexes := normalizeUsageScopeAuthIndexes(scope.AuthIndexes)
	apiGroupKey := strings.TrimSpace(scope.APIGroupKey)
	if apiGroupKey == "" || len(authIndexes) == 0 {
		return []entities.UsageIdentity{}, nil
	}
	items, err := repository.ListActiveAuthFileUsageIdentitiesByAuthIndexes(ctx, s.db, authIndexes)
	if err != nil {
		return nil, err
	}
	stats, err := repository.ListScopedAuthFileUsageIdentityStats(ctx, s.db, apiGroupKey, authIndexes)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = applyScopedUsageIdentityStats(items[index], stats[items[index].Identity])
	}
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(items[i].Name))
		right := strings.ToLower(strings.TrimSpace(items[j].Name))
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
	return items, nil
}

func (s *usageIdentityService) listScopedAuthFileUsageIdentitiesPage(ctx context.Context, request ListUsageIdentitiesRequest) (ListUsageIdentitiesResponse, error) {
	if request.AuthType != nil && *request.AuthType != entities.UsageIdentityAuthTypeAuthFile {
		return ListUsageIdentitiesResponse{Items: []entities.UsageIdentity{}, TypeCounts: []UsageIdentityTypeCount{}, CredentialHealth: []UsageCredentialHealthSnapshot{}}, nil
	}
	items, err := s.listScopedAuthFileUsageIdentities(ctx)
	if err != nil {
		return ListUsageIdentitiesResponse{}, err
	}
	if request.ActiveOnly != nil && *request.ActiveOnly {
		items = filterEnabledUsageIdentities(items)
	}
	typeCounts := scopedUsageIdentityTypeCounts(items)
	items = filterUsageIdentityTypes(items, request.Types)
	sortScopedUsageIdentities(items, request.Sort)

	total := int64(len(items))
	page := request.Page
	if page <= 0 {
		page = 1
	}
	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		items = []entities.UsageIdentity{}
	} else {
		end := start + pageSize
		if end > len(items) {
			end = len(items)
		}
		items = items[start:end]
	}
	// 最近事件缓存只按认证文件聚合，未包含 API Key 维度；Viewer 不返回该全局健康图。
	return ListUsageIdentitiesResponse{Items: items, Total: total, TypeCounts: typeCounts, CredentialHealth: []UsageCredentialHealthSnapshot{}}, nil
}

func applyScopedUsageIdentityStats(identity entities.UsageIdentity, stats repository.ScopedUsageIdentityStats) entities.UsageIdentity {
	identity.TotalRequests = stats.TotalRequests
	identity.SuccessCount = stats.SuccessCount
	identity.FailureCount = stats.FailureCount
	identity.InputTokens = stats.InputTokens
	identity.OutputTokens = stats.OutputTokens
	identity.ReasoningTokens = stats.ReasoningTokens
	identity.CachedTokens = stats.CachedTokens
	identity.TotalTokens = stats.TotalTokens
	identity.FirstUsedAt = stats.FirstUsedAt
	identity.LastUsedAt = stats.LastUsedAt
	identity.LastAggregatedUsageEventID = 0
	identity.StatsUpdatedAt = nil
	return identity
}

func filterEnabledUsageIdentities(items []entities.UsageIdentity) []entities.UsageIdentity {
	result := make([]entities.UsageIdentity, 0, len(items))
	for _, item := range items {
		if item.Disabled != nil && *item.Disabled {
			continue
		}
		result = append(result, item)
	}
	return result
}

func filterUsageIdentityTypes(items []entities.UsageIdentity, types []string) []entities.UsageIdentity {
	if len(types) == 0 {
		return items
	}
	allowed := make(map[string]struct{}, len(types))
	for _, value := range types {
		value = strings.TrimSpace(value)
		if value != "" {
			allowed[value] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return items
	}
	result := make([]entities.UsageIdentity, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.Type]; ok {
			result = append(result, item)
		}
	}
	return result
}

func scopedUsageIdentityTypeCounts(items []entities.UsageIdentity) []UsageIdentityTypeCount {
	counts := map[string]int64{}
	for _, item := range items {
		counts[item.Type]++
	}
	types := make([]string, 0, len(counts))
	for value := range counts {
		types = append(types, value)
	}
	sort.Strings(types)
	result := make([]UsageIdentityTypeCount, 0, len(types))
	for _, value := range types {
		result = append(result, UsageIdentityTypeCount{Type: value, Count: counts[value]})
	}
	return result
}

func sortScopedUsageIdentities(items []entities.UsageIdentity, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		switch sortBy {
		case repository.UsageIdentityPageSortPriority:
			leftPriority, rightPriority := scopedUsageIdentityPriority(left), scopedUsageIdentityPriority(right)
			if leftPriority != rightPriority {
				return leftPriority > rightPriority
			}
		case repository.UsageIdentityPageSortTotalTokens:
			if left.TotalTokens != right.TotalTokens {
				return left.TotalTokens > right.TotalTokens
			}
		case repository.UsageIdentityPageSortLastUsedAt:
			if left.LastUsedAt == nil || right.LastUsedAt == nil {
				return left.LastUsedAt != nil
			}
			if !left.LastUsedAt.Equal(*right.LastUsedAt) {
				return left.LastUsedAt.After(*right.LastUsedAt)
			}
		default:
			if left.TotalRequests != right.TotalRequests {
				return left.TotalRequests > right.TotalRequests
			}
		}
		leftName := strings.ToLower(strings.TrimSpace(left.Name))
		rightName := strings.ToLower(strings.TrimSpace(right.Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return left.ID < right.ID
	})
}

func scopedUsageIdentityPriority(item entities.UsageIdentity) int {
	if item.Priority == nil {
		return -1
	}
	return *item.Priority
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
