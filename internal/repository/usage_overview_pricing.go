package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type usageOverviewStatProjection struct {
	BucketStart             time.Time
	APIGroupKey             string
	Model                   string
	AuthIndex               string
	ModelAlias              string
	ServiceTier             string
	ResponseServiceTier     string
	ReasoningEffort         string
	Endpoint                string
	ExecutorType            string
	RequestCount            int64
	SuccessCount            int64
	FailureCount            int64
	InputTokens             int64
	ReasoningTokens         int64
	CacheReadTokens         int64
	CacheCreationTokens     int64
	TotalTokens             int64
	CostUncachedInputTokens int64
	CostOutputTokens        int64
	CostCacheReadTokens     int64
	CostCacheCreationTokens int64
}

// 标准 SQL CASE 先逐行完成计费 Token 的非负与普通输入归一化，再按启用规则所需维度合并。
const usageOverviewStatProjectionAggregateColumns = `
	SUM(request_count) AS request_count,
	SUM(success_count) AS success_count,
	SUM(failure_count) AS failure_count,
	SUM(input_tokens) AS input_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(CASE
		WHEN (CASE WHEN input_tokens > 0 THEN input_tokens ELSE 0 END) -
			(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) -
			(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END) > 0
		THEN (CASE WHEN input_tokens > 0 THEN input_tokens ELSE 0 END) -
			(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) -
			(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END)
		ELSE 0
	END) AS cost_uncached_input_tokens,
	SUM(CASE WHEN output_tokens > 0 THEN output_tokens ELSE 0 END) AS cost_output_tokens,
	SUM(CASE WHEN cache_read_tokens > 0 THEN cache_read_tokens ELSE 0 END) AS cost_cache_read_tokens,
	SUM(CASE WHEN cache_creation_tokens > 0 THEN cache_creation_tokens ELSE 0 END) AS cost_cache_creation_tokens`

func loadUsageOverviewHourlyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, activeFields pricing.ActiveFields) ([]usageOverviewStatProjection, error) {
	query := db.Model(&entities.UsageOverviewHourlyStat{})
	return loadUsageOverviewStatProjection(query, filter, start, end, "hourly", activeFields)
}

func loadUsageOverviewDailyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, activeFields pricing.ActiveFields) ([]usageOverviewStatProjection, error) {
	query := db.Model(&entities.UsageOverviewDailyStat{})
	return loadUsageOverviewStatProjection(query, filter, start, end, "daily", activeFields)
}

func loadUsageOverviewStatProjection(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string, activeFields pricing.ActiveFields) ([]usageOverviewStatProjection, error) {
	rows := make([]usageOverviewStatProjection, 0)
	dimensionColumns := append([]string{"bucket_start"}, UsagePricingDimensionColumns(activeFields)...)
	selectColumns := strings.Join(dimensionColumns, ", ") + ", " + usageOverviewStatProjectionAggregateColumns
	query = query.
		Select(selectColumns).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end))
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if err := query.Group(strings.Join(dimensionColumns, ", ")).Order("bucket_start asc").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s projection: %w", grain, err)
	}
	return rows, nil
}

func applyUsageOverviewStatToOverview(overview *dto.UsageOverviewRecord, row usageOverviewStatProjection, bucketByDay bool, costResolver pricing.Resolver) {
	applyUsageOverviewStatToSnapshotTotals(overview.Usage, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)
	result := calculateUsageOverviewProjectionCost(costResolver, row)
	if !result.Available {
		overview.Summary.CostAvailable = false
	}
	rowCost := result.Cost.TotalCostUSD
	applyUsageOverviewStatToSummary(overview, row.InputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, rowCost)

	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)
}

func calculateUsageOverviewProjectionCost(costResolver pricing.Resolver, row usageOverviewStatProjection) pricing.CostResult {
	return costResolver.Calculate(newUsagePricingCostSubject(
		row.APIGroupKey,
		row.Model,
		row.AuthIndex,
		row.ModelAlias,
		row.ServiceTier,
		row.ResponseServiceTier,
		row.ReasoningEffort,
		row.Endpoint,
		row.ExecutorType,
		row.CostUncachedInputTokens+row.CostCacheReadTokens+row.CostCacheCreationTokens,
		row.CostOutputTokens,
		row.CostCacheReadTokens,
		row.CostCacheCreationTokens,
	))
}
