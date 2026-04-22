package service

import (
	"context"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type usageService struct {
	db *gorm.DB
}

func NewUsageService(db *gorm.DB) UsageProvider {
	return &usageService{db: db}
}

func (s *usageService) GetUsage(context.Context) (*cpa.StatisticsSnapshot, error) {
	return repository.BuildUsageSnapshot(s.db)
}

func (s *usageService) GetUsageWithFilter(_ context.Context, filter UsageFilter) (*cpa.StatisticsSnapshot, error) {
	return repository.BuildUsageSnapshotWithFilter(s.db, repository.UsageQueryFilter{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
}
