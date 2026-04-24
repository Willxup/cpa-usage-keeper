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

func (s *usageService) GetUsageWithFilter(_ context.Context, filter UsageFilter) (*cpa.StatisticsSnapshot, error) {
	return repository.BuildUsageSnapshotWithFilter(s.db, repository.UsageQueryFilter{
		Range:     filter.Range,
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
}

func (s *usageService) GetUsageOverview(_ context.Context, filter UsageFilter) (*UsageOverviewSnapshot, error) {
	overview, err := repository.BuildUsageOverviewWithFilter(s.db, repository.UsageQueryFilter{
		Range:     filter.Range,
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
	if err != nil {
		return nil, err
	}
	return &UsageOverviewSnapshot{
		Usage: overview.Usage,
		Summary: UsageOverviewSummary{
			RequestCount:    overview.Summary.RequestCount,
			TokenCount:      overview.Summary.TokenCount,
			WindowMinutes:   overview.Summary.WindowMinutes,
			RPM:             overview.Summary.RPM,
			TPM:             overview.Summary.TPM,
			TotalCost:       overview.Summary.TotalCost,
			CostAvailable:   overview.Summary.CostAvailable,
			CachedTokens:    overview.Summary.CachedTokens,
			ReasoningTokens: overview.Summary.ReasoningTokens,
		},
		Series:       mapUsageOverviewSeries(overview.Series),
		HourlySeries: mapUsageOverviewSeries(overview.HourlySeries),
		DailySeries:  mapUsageOverviewSeries(overview.DailySeries),
		Health: UsageOverviewHealth{
			TotalSuccess: overview.Health.TotalSuccess,
			TotalFailure: overview.Health.TotalFailure,
			SuccessRate:  overview.Health.SuccessRate,
			BlockDetails: func() []UsageOverviewHealthBlock {
				blocks := make([]UsageOverviewHealthBlock, 0, len(overview.Health.BlockDetails))
				for _, block := range overview.Health.BlockDetails {
					blocks = append(blocks, UsageOverviewHealthBlock{
						StartTime: block.StartTime,
						EndTime:   block.EndTime,
						Success:   block.Success,
						Failure:   block.Failure,
						Rate:      block.Rate,
					})
				}
				return blocks
			}(),
		},
	}, nil
}

func mapUsageOverviewSeries(series repository.UsageOverviewSeriesRecord) UsageOverviewSeries {
	models := make(map[string]UsageOverviewSeries, len(series.Models))
	for model, modelSeries := range series.Models {
		models[model] = mapUsageOverviewSeries(modelSeries)
	}
	return UsageOverviewSeries{
		Requests:        series.Requests,
		Tokens:          series.Tokens,
		RPM:             series.RPM,
		TPM:             series.TPM,
		Cost:            series.Cost,
		InputTokens:     series.InputTokens,
		OutputTokens:    series.OutputTokens,
		CachedTokens:    series.CachedTokens,
		ReasoningTokens: series.ReasoningTokens,
		Models:          models,
	}
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

func (s *usageService) GetUsageAnalysis(_ context.Context, filter UsageFilter) (*UsageAnalysisSnapshot, error) {
	apiRows, modelRows, err := repository.ListUsageAnalysisWithFilter(s.db, repository.UsageQueryFilter{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
	if err != nil {
		return nil, err
	}

	apis := make([]UsageAnalysisAPIStat, 0, len(apiRows))
	for _, row := range apiRows {
		models := make([]UsageAnalysisModelStat, 0, len(row.Models))
		for _, model := range row.Models {
			models = append(models, UsageAnalysisModelStat{
				Model:              model.Model,
				TotalRequests:      model.TotalRequests,
				SuccessCount:       model.SuccessCount,
				FailureCount:       model.FailureCount,
				TotalTokens:        model.TotalTokens,
				InputTokens:        model.InputTokens,
				OutputTokens:       model.OutputTokens,
				ReasoningTokens:    model.ReasoningTokens,
				CachedTokens:       model.CachedTokens,
				TotalLatencyMS:     model.TotalLatencyMS,
				LatencySampleCount: model.LatencySampleCount,
			})
		}
		apis = append(apis, UsageAnalysisAPIStat{
			APIKey:          row.APIGroupKey,
			DisplayName:     row.DisplayName,
			TotalRequests:   row.TotalRequests,
			SuccessCount:    row.SuccessCount,
			FailureCount:    row.FailureCount,
			TotalTokens:     row.TotalTokens,
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			ReasoningTokens: row.ReasoningTokens,
			CachedTokens:    row.CachedTokens,
			Models:          models,
		})
	}

	models := make([]UsageAnalysisModelStat, 0, len(modelRows))
	for _, row := range modelRows {
		models = append(models, UsageAnalysisModelStat{
			Model:              row.Model,
			TotalRequests:      row.TotalRequests,
			SuccessCount:       row.SuccessCount,
			FailureCount:       row.FailureCount,
			TotalTokens:        row.TotalTokens,
			InputTokens:        row.InputTokens,
			OutputTokens:       row.OutputTokens,
			ReasoningTokens:    row.ReasoningTokens,
			CachedTokens:       row.CachedTokens,
			TotalLatencyMS:     row.TotalLatencyMS,
			LatencySampleCount: row.LatencySampleCount,
		})
	}

	return &UsageAnalysisSnapshot{APIs: apis, Models: models}, nil
}
