package repository

import (
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
	"time"
)

// 比较图复用已经计算的 row cost，不重复读取数据或执行计价规则。
func applyUsageOverviewComparison(comparisons *dto.UsageOverviewComparisonsRecord, model, apiKey string, row dto.UsageComparisonItemRecord) {
	addUsageOverviewComparison(comparisons.Models, normalizeUsageOverviewDimension(model), row)
	addUsageOverviewComparison(comparisons.APIKeys, normalizeUsageOverviewDimension(apiKey), row)
}

func addUsageOverviewComparison(items map[string]*dto.UsageComparisonItemRecord, key string, row dto.UsageComparisonItemRecord) {
	item := items[key]
	if item == nil {
		item = &dto.UsageComparisonItemRecord{Key: key, CostAvailable: true}
		items[key] = item
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

// 主曲线保持最小分组；比较查询不带时间桶，避免长范围乘以 Key 数量后产生大量中间行。
// 两者共享原来的范围规划，边界事件由调用方读取一次并直接补入比较结果。
func loadAndApplyUsageOverviewStats(overview *dto.UsageOverviewRecord, db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, bucketByDay bool, resolver pricing.Resolver) error {
	var model any = &entities.UsageOverviewHourlyStat{}
	if grain == "daily" {
		model = &entities.UsageOverviewDailyStat{}
	}
	mainFilter := filter
	mainFilter.IncludeComparisons = false
	rows, err := loadUsageOverviewStatProjection(db.Model(model), mainFilter, start, end, grain, resolver.ActiveFields())
	if err != nil {
		return err
	}
	for _, row := range rows {
		applyUsageOverviewStatToOverview(overview, row, bucketByDay, resolver)
	}
	if overview.Comparisons == nil {
		return nil
	}
	comparisonRows, err := loadUsageOverviewStatProjection(db.Model(model), filter, start, end, grain, resolver.ActiveFields())
	if err != nil {
		return err
	}
	for _, row := range comparisonRows {
		result := calculateUsageOverviewProjectionCost(resolver, row)
		applyUsageOverviewComparison(overview.Comparisons, row.Model, row.APIGroupKey, dto.UsageComparisonItemRecord{
			Requests: row.RequestCount, Failures: row.FailureCount, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, ReasoningTokens: row.ReasoningTokens,
			TotalTokens: row.TotalTokens, CostUSD: result.Cost.TotalCostUSD, CostAvailable: result.Available,
		})
	}
	return nil
}
