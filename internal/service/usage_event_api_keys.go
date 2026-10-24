package service

import (
	"context"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// UsageEventAPIKeyFilterProvider keeps event-derived candidates separate from
// the configured key roster used by the other dashboard pages.
type UsageEventAPIKeyFilterProvider interface {
	ListUsageEventAPIKeyFilterOptions(context.Context, servicedto.UsageFilter) ([]entities.CPAAPIKey, error)
}

func (s *usageService) ListUsageEventAPIKeyFilterOptions(ctx context.Context, filter servicedto.UsageFilter) ([]entities.CPAAPIKey, error) {
	return repository.ListUsageEventAPIKeyFilterOptions(s.db.WithContext(usageServiceContext(ctx)), repodto.UsageQueryFilter{
		Range:        filter.Range,
		CustomUnit:   filter.CustomUnit,
		StartTime:    filter.StartTime,
		EndTime:      filter.EndTime,
		EndExclusive: filter.EndExclusive,
		Model:        filter.Model,
		AuthIndex:    filter.AuthIndex,
		AuthType:     filter.AuthType,
		Result:       filter.Result,
	})
}
