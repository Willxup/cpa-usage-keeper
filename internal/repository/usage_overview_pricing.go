package repository

import (
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type usageOverviewStatProjection struct {
	BucketStart          time.Time
	RequestCount         int64
	SuccessCount         int64
	FailureCount         int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              *float64
	UnavailableCostCount *int64
	MissingCostCount     int64
}

// 普通总览按 bucket 汇总已存费用，同时显式暴露任何尚未回填的 NULL 行。
const usageOverviewStoredStatAggregateColumns = `
	bucket_start,
	SUM(request_count) AS request_count,
	SUM(success_count) AS success_count,
	SUM(failure_count) AS failure_count,
	SUM(input_tokens) AS input_tokens,
	SUM(output_tokens) AS output_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(cost_usd) AS cost_usd,
	SUM(unavailable_cost_count) AS unavailable_cost_count,
	SUM(CASE WHEN cost_usd IS NULL OR unavailable_cost_count IS NULL THEN 1 ELSE 0 END) AS missing_cost_count`

// loadUsageOverviewStatProjection 只读完整小时／日桶，保留 API Key 筛选和时间半开边界。
func loadUsageOverviewStatProjection(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string) ([]usageOverviewStatProjection, error) {
	rows := make([]usageOverviewStatProjection, 0)
	query = query.
		Select(usageOverviewStoredStatAggregateColumns).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end))
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	query = query.Group("bucket_start")
	query = query.Order("bucket_start asc")
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s projection: %w", grain, err)
	}
	return rows, nil
}

type usageOverviewComparisonProjection struct {
	APIGroupKey          string
	Model                string
	AuthIndex            string
	RequestCount         int64
	FailureCount         int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              *float64
	UnavailableCostCount *int64
	MissingCostCount     int64
}

// 比较视图只按四维展示所需的 model／Key／auth_index 聚合，不再依赖启用的价格规则维度。
const usageOverviewComparisonAggregateColumns = `
	model, api_group_key, auth_index,
	SUM(request_count) AS request_count,
	SUM(failure_count) AS failure_count,
	SUM(input_tokens) AS input_tokens,
	SUM(output_tokens) AS output_tokens,
	SUM(reasoning_tokens) AS reasoning_tokens,
	SUM(cache_read_tokens) AS cache_read_tokens,
	SUM(cache_creation_tokens) AS cache_creation_tokens,
	SUM(total_tokens) AS total_tokens,
	SUM(cost_usd) AS cost_usd,
	SUM(unavailable_cost_count) AS unavailable_cost_count,
	SUM(CASE WHEN cost_usd IS NULL OR unavailable_cost_count IS NULL THEN 1 ELSE 0 END) AS missing_cost_count`

// loadUsageOverviewComparisonProjection 对完整小时／日桶做独立固定维度投影，沿用原 Key 和半开时间过滤。
func loadUsageOverviewComparisonProjection(query *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, grain string) ([]usageOverviewComparisonProjection, error) {
	table := "usage_overview_hourly_stats"
	if grain == "daily" {
		table = "usage_overview_daily_stats"
	}
	query = query.Table(table).
		Select(usageOverviewComparisonAggregateColumns).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end))
	if key := strings.TrimSpace(filter.APIGroupKey); key != "" {
		query = query.Where("api_group_key = ?", key)
	}
	query = query.Group("model, api_group_key, auth_index").Order("model asc, api_group_key asc, auth_index asc")
	rows := make([]usageOverviewComparisonProjection, 0)
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview %s comparison projection: %w", grain, err)
	}
	return rows, nil
}

// applyUsageOverviewStatToOverview 保留原请求／Token 汇总，只把费用来源换成已存桶金额。
func applyUsageOverviewStatToOverview(overview *dto.UsageOverviewRecord, row usageOverviewStatProjection, bucketByDay bool) error {
	if row.MissingCostCount != 0 || row.CostUSD == nil || row.UnavailableCostCount == nil {
		return fmt.Errorf("usage overview bucket %s has unbackfilled cost", timeutil.FormatStorageTime(row.BucketStart))
	}
	if math.IsNaN(*row.CostUSD) || math.IsInf(*row.CostUSD, 0) {
		return fmt.Errorf("usage overview bucket %s has non-finite cost", timeutil.FormatStorageTime(row.BucketStart))
	}
	applyUsageOverviewStatToSnapshotTotals(overview.Usage, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)
	if *row.UnavailableCostCount > 0 {
		overview.Summary.CostAvailable = false
	}
	rowCost := *row.CostUSD
	applyUsageOverviewStatToSummary(overview, row.InputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, rowCost)

	bucketKey := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.TotalTokens, rowCost, bucketKey)
	return nil
}
