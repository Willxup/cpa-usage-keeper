package service

import (
	"context"

	"cpa-usage-keeper/internal/repository"
)

type UsageIdentityActivityReader interface {
	GetUsageIdentityActivity(context.Context, int64) (repository.UsageIdentityActivity, error)
}

func (s *usageIdentityService) GetUsageIdentityActivity(ctx context.Context, id int64) (repository.UsageIdentityActivity, error) {
	if id <= 0 {
		return repository.UsageIdentityActivity{}, ErrInvalidID
	}
	return repository.LoadUsageIdentityActivity(ctx, s.db, id, s.now())
}
