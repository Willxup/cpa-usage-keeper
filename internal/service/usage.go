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

func (s *usageService) ListUsageEvents(_ context.Context, filter UsageFilter) ([]UsageEventRecord, error) {
	rows, err := repository.ListUsageEventsWithFilter(s.db, repository.UsageQueryFilter{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
		Limit:     filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]UsageEventRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, UsageEventRecord{
			Timestamp:       row.Timestamp,
			APIGroupKey:     row.APIGroupKey,
			Model:           row.Model,
			Source:          row.Source,
			AuthIndex:       row.AuthIndex,
			Failed:          row.Failed,
			LatencyMS:       row.LatencyMS,
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			ReasoningTokens: row.ReasoningTokens,
			CachedTokens:    row.CachedTokens,
			TotalTokens:     row.TotalTokens,
		})
	}
	return result, nil
}

func (s *usageService) ListUsageCredentialStats(_ context.Context, filter UsageFilter) ([]UsageCredentialStat, error) {
	rows, err := repository.ListUsageCredentialStatsWithFilter(s.db, repository.UsageQueryFilter{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
	if err != nil {
		return nil, err
	}
	result := make([]UsageCredentialStat, 0, len(rows))
	for _, row := range rows {
		result = append(result, UsageCredentialStat{
			Source:       row.Source,
			AuthIndex:    row.AuthIndex,
			Failed:       row.Failed,
			RequestCount: row.RequestCount,
		})
	}
	return result, nil
}
