package service

import (
	"context"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/accessscope"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type usageService struct {
	db          *gorm.DB
	recentUsage *repository.UsageRecentEventCache
}

// resolvedUsageScope 是一次 usage 查询的服务端强制范围。
// 管理员只会携带可选 APIGroupKey；Viewer 同时强制绑定其登录 API Key 与认证文件 auth_index 白名单。
type resolvedUsageScope struct {
	APIGroupKey            string
	AuthIndexScopeEnforced bool
	AllowedAuthIndexes     []string
}

func NewUsageService(db *gorm.DB) UsageProvider {
	return NewUsageServiceWithRecentCache(db, nil)
}

func NewUsageServiceWithRecentCache(db *gorm.DB, recentUsage *repository.UsageRecentEventCache) UsageProvider {
	return &usageService{db: db, recentUsage: recentUsage}
}

func usageServiceContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *usageService) resolveAPIGroupKey(ctx context.Context, apiKeyID string) (string, error) {
	ctx = usageServiceContext(ctx)
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return "", nil
	}
	parsedID, err := strconv.ParseInt(apiKeyID, 10, 64)
	if err != nil || parsedID <= 0 {
		return "", ErrInvalidID
	}
	apiKey, err := repository.FindActiveCPAAPIKeyByID(s.db.WithContext(ctx), parsedID)
	if err != nil {
		return "", err
	}
	return apiKey.APIKey, nil
}

func (s *usageService) resolveUsageScope(ctx context.Context, requestedAPIKeyID string) (resolvedUsageScope, error) {
	ctx = usageServiceContext(ctx)
	if viewerScope, ok := accessscope.ViewerScopeFromContext(ctx); ok {
		apiGroupKey := strings.TrimSpace(viewerScope.APIGroupKey)
		authIndexes := normalizeUsageScopeAuthIndexes(viewerScope.AuthIndexes)
		if apiGroupKey == "" || len(authIndexes) == 0 {
			return resolvedUsageScope{}, ErrAPIKeyAuthFileScopeNotConfigured
		}
		return resolvedUsageScope{
			APIGroupKey:            apiGroupKey,
			AuthIndexScopeEnforced: true,
			AllowedAuthIndexes:     authIndexes,
		}, nil
	}
	apiGroupKey, err := s.resolveAPIGroupKey(ctx, requestedAPIKeyID)
	if err != nil {
		return resolvedUsageScope{}, err
	}
	return resolvedUsageScope{APIGroupKey: apiGroupKey}, nil
}

func normalizeUsageScopeAuthIndexes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	authIndexes := make([]string, 0, len(values))
	for _, value := range values {
		authIndex := strings.TrimSpace(value)
		if authIndex == "" {
			continue
		}
		if _, exists := seen[authIndex]; exists {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
	}
	return authIndexes
}

func newRepositoryUsageFilter(filter servicedto.UsageFilter, scope resolvedUsageScope) repodto.UsageQueryFilter {
	return repodto.UsageQueryFilter{
		Range:                  filter.Range,
		CustomUnit:             filter.CustomUnit,
		StartTime:              filter.StartTime,
		EndTime:                filter.EndTime,
		EndExclusive:           filter.EndExclusive,
		QueryNow:               filter.QueryNow,
		RealtimeWindow:         filter.RealtimeWindow,
		RealtimeEndTime:        filter.RealtimeEndTime,
		Limit:                  filter.Limit,
		Page:                   filter.Page,
		PageSize:               filter.PageSize,
		Offset:                 filter.Offset,
		Model:                  filter.Model,
		AuthIndex:              filter.AuthIndex,
		APIGroupKey:            scope.APIGroupKey,
		Result:                 filter.Result,
		AuthIndexScopeEnforced: scope.AuthIndexScopeEnforced,
		AllowedAuthIndexes:     scope.AllowedAuthIndexes,
	}
}

// Usage 页面里的 Overview tab 下传时间窗口和全局 API-Key，仓储层负责构建 overview 聚合。
func (s *usageService) GetUsageOverview(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	overview, err := repository.BuildUsageOverviewWithFilterAndRecentCache(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope), s.recentUsage)
	if err != nil {
		return nil, err
	}
	return &servicedto.UsageOverviewSnapshot{
		Usage: overview.Usage,
		Summary: servicedto.UsageOverviewSummary{
			RequestCount:          overview.Summary.RequestCount,
			TokenCount:            overview.Summary.TokenCount,
			WindowMinutes:         overview.Summary.WindowMinutes,
			RPM:                   overview.Summary.RPM,
			TPM:                   overview.Summary.TPM,
			TotalCost:             overview.Summary.TotalCost,
			CostAvailable:         overview.Summary.CostAvailable,
			InputTokens:           overview.Summary.InputTokens,
			CacheReadTokens:       overview.Summary.CacheReadTokens,
			CacheCreationTokens:   overview.Summary.CacheCreationTokens,
			ReasoningTokens:       overview.Summary.ReasoningTokens,
			DailyAverageRequests:  overview.Summary.DailyAverageRequests,
			DailyAverageTokens:    overview.Summary.DailyAverageTokens,
			DailyAverageCost:      overview.Summary.DailyAverageCost,
			DailyAverageRangeDays: overview.Summary.DailyAverageRangeDays,
		},
		Series: mapUsageOverviewSeries(overview.Series),
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

func (s *usageService) GetUsageOverviewRealtime(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	realtime, err := repository.BuildUsageOverviewRealtimeWithFilterAndRecentCache(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope), s.recentUsage)
	if err != nil {
		return nil, err
	}
	result := mapUsageOverviewRealtime(realtime)
	return &result, nil
}

func mapUsageOverviewSeries(series repodto.UsageOverviewSeriesRecord) servicedto.UsageOverviewSeries {
	return servicedto.UsageOverviewSeries{
		Requests:      series.Requests,
		Tokens:        series.Tokens,
		RPM:           series.RPM,
		TPM:           series.TPM,
		Cost:          series.Cost,
		CacheReadRate: series.CacheReadRate,
	}
}

func mapUsageOverviewRealtime(realtime repodto.UsageOverviewRealtimeRecord) servicedto.UsageOverviewRealtime {
	return servicedto.UsageOverviewRealtime{
		Window:               realtime.Window,
		BucketSeconds:        realtime.BucketSeconds,
		WindowStart:          realtime.WindowStart,
		WindowEnd:            realtime.WindowEnd,
		TokenVelocity:        mapRealtimeTokenVelocity(realtime.TokenVelocity),
		ResponseLevel:        mapRealtimeResponseLevel(realtime.ResponseLevel),
		ResponseDistribution: mapRealtimeResponseDistribution(realtime.ResponseDistribution),
		CurrentUsage:         mapRealtimeCurrentUsage(realtime.CurrentUsage),
		RequestLevel:         mapRealtimeRequestLevel(realtime.RequestLevel),
		CacheLevel:           mapRealtimeCacheLevel(realtime.CacheLevel),
	}
}

func mapRealtimeTokenVelocity(points []repodto.RealtimeTokenVelocityPointRecord) []servicedto.RealtimeTokenVelocityPoint {
	result := make([]servicedto.RealtimeTokenVelocityPoint, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeTokenVelocityPoint{
			Bucket:          point.Bucket,
			TokensPerMinute: point.TokensPerMinute,
			Tokens:          point.Tokens,
			CostUSD:         point.CostUSD,
		})
	}
	return result
}

func mapRealtimeResponseLevel(points []repodto.RealtimeResponseLevelPointRecord) []servicedto.RealtimeResponseLevelPoint {
	result := make([]servicedto.RealtimeResponseLevelPoint, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeResponseLevelPoint{
			Bucket:       point.Bucket,
			TTFTP50MS:    point.TTFTP50MS,
			TTFTP95MS:    point.TTFTP95MS,
			LatencyP50MS: point.LatencyP50MS,
			LatencyP95MS: point.LatencyP95MS,
		})
	}
	return result
}

func mapRealtimeResponseDistribution(distribution repodto.RealtimeResponseDistributionRecord) servicedto.RealtimeResponseDistribution {
	return servicedto.RealtimeResponseDistribution{
		TTFT:    mapRealtimeResponseDistributionSeries(distribution.TTFT),
		Latency: mapRealtimeResponseDistributionSeries(distribution.Latency),
	}
}

func mapRealtimeResponseDistributionSeries(series repodto.RealtimeResponseDistributionSeriesRecord) servicedto.RealtimeResponseDistributionSeries {
	return servicedto.RealtimeResponseDistributionSeries{
		AverageLine:    mapRealtimeResponseAveragePoints(series.AverageLine),
		Particles:      mapRealtimeResponseParticles(series.Particles),
		TotalParticles: series.TotalParticles,
		Sampled:        series.Sampled,
		MaxParticles:   series.MaxParticles,
	}
}

func mapRealtimeResponseAveragePoints(points []repodto.RealtimeResponseAveragePointRecord) []servicedto.RealtimeResponseAveragePoint {
	result := make([]servicedto.RealtimeResponseAveragePoint, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeResponseAveragePoint{
			Bucket: point.Bucket,
			AvgMS:  point.AvgMS,
		})
	}
	return result
}

func mapRealtimeResponseParticles(points []repodto.RealtimeResponseParticleRecord) []servicedto.RealtimeResponseParticle {
	result := make([]servicedto.RealtimeResponseParticle, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeResponseParticle{
			Bucket:    point.Bucket,
			Timestamp: point.Timestamp,
			MS:        point.MS,
			Count:     point.Count,
		})
	}
	return result
}

func mapRealtimeCurrentUsage(current repodto.RealtimeCurrentUsageRecord) servicedto.RealtimeCurrentUsage {
	return servicedto.RealtimeCurrentUsage{
		Models:      mapRealtimeUsageTopItems(current.Models),
		APIKeys:     mapRealtimeUsageTopItems(current.APIKeys),
		AuthFiles:   mapRealtimeUsageTopItems(current.AuthFiles),
		AIProviders: mapRealtimeUsageTopItems(current.AIProviders),
	}
}

func mapRealtimeUsageTopItems(items []repodto.RealtimeUsageTopItemRecord) []servicedto.RealtimeUsageTopItem {
	result := make([]servicedto.RealtimeUsageTopItem, 0, len(items))
	for _, item := range items {
		result = append(result, servicedto.RealtimeUsageTopItem{
			Key:      item.Key,
			Label:    item.Label,
			Tokens:   item.Tokens,
			Requests: item.Requests,
			CostUSD:  item.CostUSD,
			Share:    item.Share,
		})
	}
	return result
}

func mapRealtimeRequestLevel(points []repodto.RealtimeRequestLevelPointRecord) []servicedto.RealtimeRequestLevelPoint {
	result := make([]servicedto.RealtimeRequestLevelPoint, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeRequestLevelPoint{
			Bucket:            point.Bucket,
			RequestsPerMinute: point.RequestsPerMinute,
			Requests:          point.Requests,
		})
	}
	return result
}

func mapRealtimeCacheLevel(points []repodto.RealtimeCacheLevelPointRecord) []servicedto.RealtimeCacheLevelPoint {
	result := make([]servicedto.RealtimeCacheLevelPoint, 0, len(points))
	for _, point := range points {
		result = append(result, servicedto.RealtimeCacheLevelPoint{
			Bucket:              point.Bucket,
			CacheReadRate:       point.CacheReadRate,
			CacheReadTokens:     point.CacheReadTokens,
			CacheCreationTokens: point.CacheCreationTokens,
			InputTokens:         point.InputTokens,
		})
	}
	return result
}

func (s *usageService) GetAnalysis(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	record, err := repository.BuildAnalysisWithFilter(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope))
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
			Bucket:              bucket.Bucket,
			InputTokens:         bucket.InputTokens,
			OutputTokens:        bucket.OutputTokens,
			CacheReadTokens:     bucket.CacheReadTokens,
			CacheCreationTokens: bucket.CacheCreationTokens,
			ReasoningTokens:     bucket.ReasoningTokens,
			TotalTokens:         bucket.TotalTokens,
			Requests:            bucket.Requests,
			CostUSD:             bucket.CostUSD,
			CostAvailable:       bucket.CostAvailable,
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
			APIKey:              cell.APIKey,
			Model:               cell.Model,
			InputTokens:         cell.InputTokens,
			OutputTokens:        cell.OutputTokens,
			CacheReadTokens:     cell.CacheReadTokens,
			CacheCreationTokens: cell.CacheCreationTokens,
			ReasoningTokens:     cell.ReasoningTokens,
			TotalTokens:         cell.TotalTokens,
			Requests:            cell.Requests,
			CostUSD:             cell.CostUSD,
			CostAvailable:       cell.CostAvailable,
		})
	}
	modelEfficiency := make([]servicedto.AnalysisModelEfficiencyItem, 0, len(record.ModelEfficiency))
	for _, item := range record.ModelEfficiency {
		modelEfficiency = append(modelEfficiency, servicedto.AnalysisModelEfficiencyItem{
			Model:                  item.Model,
			Requests:               item.Requests,
			InputTokens:            item.InputTokens,
			OutputTokens:           item.OutputTokens,
			CacheReadTokens:        item.CacheReadTokens,
			CacheCreationTokens:    item.CacheCreationTokens,
			ReasoningTokens:        item.ReasoningTokens,
			TotalTokens:            item.TotalTokens,
			CostUSD:                item.CostUSD,
			CostAvailable:          item.CostAvailable,
			CostPerRequestUSD:      item.CostPerRequestUSD,
			OutputTokensPerRequest: item.OutputTokensPerRequest,
			CacheReadRate:          item.CacheReadRate,
		})
	}
	latencyPoints := make([]servicedto.AnalysisLatencyPoint, 0, len(record.LatencyDiagnostics.Points))
	for _, point := range record.LatencyDiagnostics.Points {
		latencyPoints = append(latencyPoints, servicedto.AnalysisLatencyPoint{
			TTFTMS:    point.TTFTMS,
			LatencyMS: point.LatencyMS,
		})
	}
	latencyDensity := make([]servicedto.AnalysisLatencyDensityCell, 0, len(record.LatencyDiagnostics.Density))
	for _, cell := range record.LatencyDiagnostics.Density {
		latencyDensity = append(latencyDensity, servicedto.AnalysisLatencyDensityCell{
			TTFTMinMS:    cell.TTFTMinMS,
			TTFTMaxMS:    cell.TTFTMaxMS,
			LatencyMinMS: cell.LatencyMinMS,
			LatencyMaxMS: cell.LatencyMaxMS,
			Count:        cell.Count,
			Intensity:    cell.Intensity,
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
		CostBreakdown: servicedto.AnalysisCostBreakdown{
			UncachedInputCostUSD: record.CostBreakdown.UncachedInputCostUSD,
			CacheReadCostUSD:     record.CostBreakdown.CacheReadCostUSD,
			CacheWriteCostUSD:    record.CostBreakdown.CacheWriteCostUSD,
			OutputCostUSD:        record.CostBreakdown.OutputCostUSD,
			TotalCostUSD:         record.CostBreakdown.TotalCostUSD,
			CostAvailable:        record.CostBreakdown.CostAvailable,
		},
		ModelEfficiency: modelEfficiency,
		LatencyDiagnostics: servicedto.AnalysisLatencyDiagnostics{
			Points:       latencyPoints,
			Density:      latencyDensity,
			TotalPoints:  record.LatencyDiagnostics.TotalPoints,
			Sampled:      record.LatencyDiagnostics.Sampled,
			P95TTFTMS:    record.LatencyDiagnostics.P95TTFTMS,
			P95LatencyMS: record.LatencyDiagnostics.P95LatencyMS,
			MaxTTFTMS:    record.LatencyDiagnostics.MaxTTFTMS,
			MaxLatencyMS: record.LatencyDiagnostics.MaxLatencyMS,
		},
	}
}

func mapAnalysisCompositionRecord(item repodto.AnalysisCompositionRecord) servicedto.AnalysisCompositionItem {
	return servicedto.AnalysisCompositionItem{
		Key:                 item.Key,
		Label:               item.Label,
		TotalTokens:         item.TotalTokens,
		Requests:            item.Requests,
		InputTokens:         item.InputTokens,
		OutputTokens:        item.OutputTokens,
		CacheReadTokens:     item.CacheReadTokens,
		CacheCreationTokens: item.CacheCreationTokens,
		ReasoningTokens:     item.ReasoningTokens,
		CostUSD:             item.CostUSD,
		CostAvailable:       item.CostAvailable,
	}
}

// Usage 页面里的 Request Event Log tab 下传分页、列表筛选条件和全局 API-Key。
func (s *usageService) ListUsageEvents(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	page, err := repository.ListUsageEventsWithFilter(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope))
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
			ModelAlias:          row.ModelAlias,
			ReasoningEffort:     row.ReasoningEffort,
			ServiceTier:         row.ServiceTier,
			ResponseServiceTier: row.ResponseServiceTier,
			ExecutorType:        row.ExecutorType,
			Endpoint:            row.Endpoint,
			AuthType:            row.AuthType,
			RequestID:           row.RequestID,
			Provider:            row.Provider,
			Source:              row.Source,
			AuthIndex:           row.AuthIndex,
			Failed:              row.Failed,
			LatencyMS:           row.LatencyMS,
			TTFTMS:              row.TTFTMS,
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			ReasoningTokens:     row.ReasoningTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
			TotalTokens:         row.TotalTokens,
			CostUSD:             row.CostUSD,
			CostAvailable:       row.CostAvailable,
			PricingStyle:        row.PricingStyle,
		})
	}
	return &servicedto.UsageEventsPage{Events: result, Models: page.Models, TotalCount: page.TotalCount, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages}, nil
}

// StreamUsageEvents 使用 Request Event Log 相同筛选条件逐行导出，不应用分页。
func (s *usageService) StreamUsageEvents(ctx context.Context, filter servicedto.UsageFilter, emit func(servicedto.UsageEventRecord) error) error {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return err
	}
	return repository.StreamUsageEventsWithFilter(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope), func(row repodto.UsageEventRecord) error {
		return emit(servicedto.UsageEventRecord{
			ID:                  row.ID,
			Timestamp:           row.Timestamp,
			APIGroupKey:         row.APIGroupKey,
			Model:               row.Model,
			ModelAlias:          row.ModelAlias,
			ReasoningEffort:     row.ReasoningEffort,
			ServiceTier:         row.ServiceTier,
			ResponseServiceTier: row.ResponseServiceTier,
			ExecutorType:        row.ExecutorType,
			Endpoint:            row.Endpoint,
			AuthType:            row.AuthType,
			RequestID:           row.RequestID,
			Provider:            row.Provider,
			Source:              row.Source,
			AuthIndex:           row.AuthIndex,
			Failed:              row.Failed,
			LatencyMS:           row.LatencyMS,
			TTFTMS:              row.TTFTMS,
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			ReasoningTokens:     row.ReasoningTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
			TotalTokens:         row.TotalTokens,
			CostUSD:             row.CostUSD,
			CostAvailable:       row.CostAvailable,
			PricingStyle:        row.PricingStyle,
		})
	})
}

// Request Event Log 的 model 筛选项只应用调用方传入的时间窗口；独立筛选项接口当前传空 filter。
func (s *usageService) ListUsageEventFilterOptions(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	ctx = usageServiceContext(ctx)
	scope, err := s.resolveUsageScope(ctx, filter.APIKeyID)
	if err != nil {
		return nil, err
	}
	options, err := repository.ListUsageEventFilterOptionsWithFilter(s.db.WithContext(ctx), newRepositoryUsageFilter(filter, scope))
	if err != nil {
		return nil, err
	}
	return &servicedto.UsageEventFilterOptions{Models: options.Models}, nil
}
