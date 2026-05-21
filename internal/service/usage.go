package service

import (
	"context"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type usageService struct {
	db *gorm.DB
}

func NewUsageService(db *gorm.DB) UsageProvider {
	return &usageService{db: db}
}

func (s *usageService) resolveAPIGroupKey(apiKeyID string) (string, error) {
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return "", nil
	}
	parsedID, err := strconv.ParseInt(apiKeyID, 10, 64)
	if err != nil || parsedID <= 0 {
		return "", ErrInvalidID
	}
	apiKey, err := repository.FindActiveCPAAPIKeyByID(s.db, parsedID)
	if err != nil {
		return "", err
	}
	return apiKey.APIKey, nil
}

func (s *usageService) GetUsageWithFilter(_ context.Context, filter servicedto.UsageFilter) (*repodto.StatisticsSnapshot, error) {
	return repository.BuildUsageSnapshotWithFilter(s.db, repodto.UsageQueryFilter{
		Range:     filter.Range,
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
}

// Usage 页面里的 Overview tab 下传时间窗口和全局 API-Key，仓储层负责构建 overview 聚合。
func (s *usageService) GetUsageOverview(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	apiGroupKey, err := s.resolveAPIGroupKey(filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	overview, err := repository.BuildUsageOverviewWithFilter(s.db, repodto.UsageQueryFilter{
		Range:       filter.Range,
		StartTime:   filter.StartTime,
		EndTime:     filter.EndTime,
		APIGroupKey: apiGroupKey,
	})
	if err != nil {
		return nil, err
	}
	return &servicedto.UsageOverviewSnapshot{
		Usage: overview.Usage,
		Summary: servicedto.UsageOverviewSummary{
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
		Health: servicedto.UsageOverviewHealth{
			TotalSuccess:  overview.Health.TotalSuccess,
			TotalFailure:  overview.Health.TotalFailure,
			SuccessRate:   overview.Health.SuccessRate,
			Rows:          overview.Health.Rows,
			Columns:       overview.Health.Columns,
			BucketSeconds: overview.Health.BucketSeconds,
			WindowStart:   overview.Health.WindowStart,
			WindowEnd:     overview.Health.WindowEnd,
			BlockDetails: func() []servicedto.UsageOverviewHealthBlock {
				blocks := make([]servicedto.UsageOverviewHealthBlock, 0, len(overview.Health.BlockDetails))
				for _, block := range overview.Health.BlockDetails {
					blocks = append(blocks, servicedto.UsageOverviewHealthBlock{
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

func mapUsageOverviewSeries(series repodto.UsageOverviewSeriesRecord) servicedto.UsageOverviewSeries {
	models := make(map[string]servicedto.UsageOverviewSeries, len(series.Models))
	for model, modelSeries := range series.Models {
		models[model] = mapUsageOverviewSeries(modelSeries)
	}
	return servicedto.UsageOverviewSeries{
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

func (s *usageService) GetAnalysis(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	apiGroupKey, err := s.resolveAPIGroupKey(filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	record, err := repository.BuildAnalysisWithFilter(s.db, repodto.UsageQueryFilter{
		Range:       filter.Range,
		StartTime:   filter.StartTime,
		EndTime:     filter.EndTime,
		APIGroupKey: apiGroupKey,
	})
	if err != nil {
		return nil, err
	}
	return mapAnalysisRecord(record), nil
}

func mapAnalysisRecord(record *repodto.AnalysisRecord) *servicedto.AnalysisSnapshot {
	if record == nil {
		return &servicedto.AnalysisSnapshot{}
	}
	tokenUsage := make([]servicedto.AnalysisTokenUsageBucket, 0, len(record.TokenUsage))
	for _, bucket := range record.TokenUsage {
		tokenUsage = append(tokenUsage, servicedto.AnalysisTokenUsageBucket{
			Bucket:          bucket.Bucket,
			InputTokens:     bucket.InputTokens,
			OutputTokens:    bucket.OutputTokens,
			CachedTokens:    bucket.CachedTokens,
			ReasoningTokens: bucket.ReasoningTokens,
			TotalTokens:     bucket.TotalTokens,
			Requests:        bucket.Requests,
		})
	}
	apiKeys := make([]servicedto.AnalysisCompositionItem, 0, len(record.APIKeyComposition))
	for _, item := range record.APIKeyComposition {
		apiKeys = append(apiKeys, mapAnalysisCompositionRecord(item))
	}
	models := make([]servicedto.AnalysisCompositionItem, 0, len(record.ModelComposition))
	for _, item := range record.ModelComposition {
		models = append(models, mapAnalysisCompositionRecord(item))
	}
	authFiles := make([]servicedto.AnalysisCompositionItem, 0, len(record.AuthFilesComposition))
	for _, item := range record.AuthFilesComposition {
		authFiles = append(authFiles, mapAnalysisCompositionRecord(item))
	}
	aiProviders := make([]servicedto.AnalysisCompositionItem, 0, len(record.AIProviderComposition))
	for _, item := range record.AIProviderComposition {
		aiProviders = append(aiProviders, mapAnalysisCompositionRecord(item))
	}
	heatmap := make([]servicedto.AnalysisHeatmapCell, 0, len(record.Heatmap))
	for _, cell := range record.Heatmap {
		heatmap = append(heatmap, servicedto.AnalysisHeatmapCell{
			APIKey:      cell.APIKey,
			Model:       cell.Model,
			TotalTokens: cell.TotalTokens,
			Requests:    cell.Requests,
		})
	}
	return &servicedto.AnalysisSnapshot{
		Granularity:           servicedto.AnalysisGranularity(record.Granularity),
		RangeStart:            record.RangeStart,
		RangeEnd:              record.RangeEnd,
		TokenUsage:            tokenUsage,
		APIKeyComposition:     apiKeys,
		ModelComposition:      models,
		AuthFilesComposition:  authFiles,
		AIProviderComposition: aiProviders,
		Heatmap:               heatmap,
	}
}

func mapAnalysisCompositionRecord(item repodto.AnalysisCompositionRecord) servicedto.AnalysisCompositionItem {
	return servicedto.AnalysisCompositionItem{
		Key:             item.Key,
		Label:           item.Label,
		TotalTokens:     item.TotalTokens,
		Requests:        item.Requests,
		InputTokens:     item.InputTokens,
		OutputTokens:    item.OutputTokens,
		CachedTokens:    item.CachedTokens,
		ReasoningTokens: item.ReasoningTokens,
	}
}

// Usage 页面里的 Request Event Log tab 下传分页、列表筛选条件和全局 API-Key。
func (s *usageService) ListUsageEvents(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	apiGroupKey, err := s.resolveAPIGroupKey(filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	page, err := repository.ListUsageEventsWithFilter(s.db, repodto.UsageQueryFilter{
		StartTime:   filter.StartTime,
		EndTime:     filter.EndTime,
		Limit:       filter.Limit,
		Page:        filter.Page,
		PageSize:    filter.PageSize,
		Offset:      filter.Offset,
		Model:       filter.Model,
		Source:      filter.Source,
		AuthIndex:   filter.AuthIndex,
		APIGroupKey: apiGroupKey,
		Result:      filter.Result,
	})
	if err != nil {
		return nil, err
	}
	result := make([]servicedto.UsageEventRecord, 0, len(page.Events))
	for _, row := range page.Events {
		result = append(result, servicedto.UsageEventRecord{
			ID:                  row.ID,
			Timestamp:           row.Timestamp,
			APIGroupKey:         row.APIGroupKey,
			Model:               row.Model,
			ReasoningEffort:     row.ReasoningEffort,
			AuthType:            row.AuthType,
			Provider:            row.Provider,
			Source:              row.Source,
			AuthIndex:           row.AuthIndex,
			Failed:              row.Failed,
			LatencyMS:           row.LatencyMS,
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			ReasoningTokens:     row.ReasoningTokens,
			CachedTokens:        row.CachedTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
			TotalTokens:         row.TotalTokens,
		})
	}
	return &servicedto.UsageEventsPage{Events: result, Models: page.Models, TotalCount: page.TotalCount, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages}, nil
}

// Usage 页面里的 Request Event Log tab 的 model 筛选项只按当前时间窗口加载候选值。
func (s *usageService) ListUsageEventFilterOptions(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	options, err := repository.ListUsageEventFilterOptionsWithFilter(s.db, repodto.UsageQueryFilter{
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
	})
	if err != nil {
		return nil, err
	}
	return &servicedto.UsageEventFilterOptions{Models: options.Models}, nil
}
