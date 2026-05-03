package service

import (
	"context"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type UsageIdentityProvider interface {
	ListUsageIdentities(context.Context) ([]models.UsageIdentity, error)
}

type usageIdentityService struct {
	db *gorm.DB
}

func NewUsageIdentityService(db *gorm.DB) UsageIdentityProvider {
	return &usageIdentityService{db: db}
}

func (s *usageIdentityService) ListUsageIdentities(ctx context.Context) ([]models.UsageIdentity, error) {
	return repository.ListUsageIdentities(ctx, s.db)
}
