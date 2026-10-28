package service

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// UsageAPIKeyOptionsProvider 与无范围的 Key 名册分离，避免影响设置及身份校验。
type UsageAPIKeyOptionsProvider interface {
	ListUsageAPIKeyOptions(context.Context, string, servicedto.UsageFilter) ([]entities.CPAAPIKey, error)
}

func (s *usageService) ListUsageAPIKeyOptions(ctx context.Context, scope string, filter servicedto.UsageFilter) ([]entities.CPAAPIKey, error) {
	return repository.ListUsageAPIKeyOptions(s.db.WithContext(usageServiceContext(ctx)), scope, repodto.UsageQueryFilter{
		Range: filter.Range, CustomUnit: filter.CustomUnit, StartTime: filter.StartTime, EndTime: filter.EndTime,
		EndExclusive: filter.EndExclusive, RealtimeWindow: filter.RealtimeWindow,
		RealtimeEndTime: filter.RealtimeEndTime, QueryNow: filter.QueryNow,
	}, s.recentUsage)
}
