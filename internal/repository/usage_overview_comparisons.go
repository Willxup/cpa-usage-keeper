package repository

import (
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	"strings"
	"time"
)

func applyUsageEventToComparisonOnly(comparisons *dto.UsageOverviewComparisonsRecord, event entities.UsageEvent, resolver pricing.Resolver, identityLookup analysisIdentityLookup) {
	failed := int64(0)
	if event.Failed {
		failed = 1
	}
	result := resolver.Calculate(UsageEventCostSubject(event))
	row := dto.UsageComparisonItemRecord{Requests: 1, Failures: failed, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, CacheReadTokens: event.CacheReadTokens, CacheCreationTokens: event.CacheCreationTokens, ReasoningTokens: event.ReasoningTokens, TotalTokens: event.TotalTokens, CostUSD: result.Cost.TotalCostUSD, CostAvailable: result.Available}
	applyUsageOverviewComparison(comparisons, event.Model, event.APIGroupKey, row)
	applyUsageOverviewIdentityComparison(comparisons, identityLookup, event.AuthIndex, row)
}

// 同一比较行的费用只计算一次，再累计到模型和 API Key 两个维度。
func applyUsageOverviewComparison(comparisons *dto.UsageOverviewComparisonsRecord, model, apiKey string, row dto.UsageComparisonItemRecord) {
	addUsageOverviewComparison(comparisons.Models, normalizeUsageOverviewDimension(model), row)
	addUsageOverviewComparison(comparisons.APIKeys, normalizeUsageOverviewDimension(apiKey), row)
}

func applyUsageOverviewIdentityComparison(comparisons *dto.UsageOverviewComparisonsRecord, identityLookup analysisIdentityLookup, authIndex string, row dto.UsageComparisonItemRecord) {
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAuthFile, strings.TrimSpace(authIndex)); ok {
		row.Label = identity.label
		addUsageOverviewComparison(comparisons.AuthFiles, identity.identity, row)
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAIProvider, strings.TrimSpace(authIndex)); ok {
		row.Label = identity.label
		addUsageOverviewComparison(comparisons.AIProviders, identity.identity, row)
	}
}

func addUsageOverviewComparison(items map[string]*dto.UsageComparisonItemRecord, key string, row dto.UsageComparisonItemRecord) {
	item := items[key]
	if item == nil {
		item = &dto.UsageComparisonItemRecord{Key: key, Label: row.Label, CostAvailable: true}
		items[key] = item
	}
	if item.Label == "" && row.Label != "" {
		item.Label = row.Label
	}
	item.Requests += row.Requests
	item.Failures += row.Failures
	item.InputTokens += row.InputTokens
	item.OutputTokens += row.OutputTokens
	item.CacheReadTokens += row.CacheReadTokens
	item.CacheCreationTokens += row.CacheCreationTokens
	item.ReasoningTokens += row.ReasoningTokens
	item.TotalTokens += row.TotalTokens
	item.CostUSD += row.CostUSD
	item.CostAvailable = item.CostAvailable && row.CostAvailable
}

// 主曲线和比较维度共享同一批带时间桶的 rollup 行；比较结果只在内存中去掉时间桶累计。
// 两者共享原来的范围规划，边界事件由调用方读取一次并直接补入比较结果。
func loadAndApplyUsageOverviewStats(overview *dto.UsageOverviewRecord, db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, bucketByDay bool, resolver pricing.Resolver) error {
	var model any = &entities.UsageOverviewHourlyStat{}
	if grain == "daily" {
		model = &entities.UsageOverviewDailyStat{}
	}
	filter.IncludeComparisons = overview.Comparisons != nil
	rows, err := loadUsageOverviewStatProjection(db.Model(model), filter, start, end, grain, resolver.ActiveFields())
	if err != nil {
		return err
	}
	var identityLookup analysisIdentityLookup
	if overview.Comparisons != nil {
		authIndexes := make([]string, 0, len(rows))
		seenAuthIndexes := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			if authIndex := strings.TrimSpace(row.AuthIndex); authIndex != "" {
				if _, seen := seenAuthIndexes[authIndex]; !seen {
					authIndexes = append(authIndexes, authIndex)
					seenAuthIndexes[authIndex] = struct{}{}
				}
			}
		}
		var err error
		identityLookup, err = loadAnalysisIdentityLookup(db, authIndexes)
		if err != nil {
			return err
		}
	}
	comparisonRows := make([]usageOverviewStatProjection, 0)
	for _, row := range rows {
		if row.ComparisonKind == 1 {
			comparisonRows = append(comparisonRows, row)
			continue
		}
		applyUsageOverviewStatToOverview(overview, row, bucketByDay, resolver)
	}
	if overview.Comparisons == nil {
		return nil
	}
	for _, row := range comparisonRows {
		result := calculateUsageOverviewProjectionCost(resolver, row)
		comparison := dto.UsageComparisonItemRecord{
			Requests: row.RequestCount, Failures: row.FailureCount, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, ReasoningTokens: row.ReasoningTokens,
			TotalTokens: row.TotalTokens, CostUSD: result.Cost.TotalCostUSD, CostAvailable: result.Available,
		}
		applyUsageOverviewComparison(overview.Comparisons, row.Model, row.APIGroupKey, comparison)
		applyUsageOverviewIdentityComparison(overview.Comparisons, identityLookup, row.AuthIndex, comparison)
	}
	return nil
}
